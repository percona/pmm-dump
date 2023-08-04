package clickhouse

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"github.com/ClickHouse/clickhouse-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"io"
	"pmm-dump/pkg/clickhouse/tsv"
	"pmm-dump/pkg/dump"
	"strings"
	"time"
)

type Source struct {
	db   *sql.DB
	cfg  Config
	tx   *sql.Tx
	ct   []*sql.ColumnType
	stmt *sql.Stmt
}

func NewSource(ctx context.Context, cfg Config) (*Source, error) {
	db, err := sql.Open("clickhouse", cfg.ConnectionURL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			return nil, errors.Errorf("exception: [%d] %s \n%s\n", exception.Code, exception.Message, exception.StackTrace)
		} else {
			return nil, err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	ct, err := columnTypes(db)
	if err != nil {
		return nil, err
	}

	stmt, err := prepareInsertStatement(tx, len(ct))
	if err != nil {
		return nil, err
	}
	return &Source{
		cfg:  cfg,
		db:   db,
		tx:   tx,
		ct:   ct,
		stmt: stmt,
	}, nil
}

func columnTypes(db *sql.DB) ([]*sql.ColumnType, error) {
	rows, err := db.Query("SELECT * FROM metrics LIMIT 1")
	if err != nil {
		return nil, err
	}
	return rows.ColumnTypes()
}

func (s Source) Type() dump.SourceType {
	return dump.ClickHouse
}

func (s Source) ReadChunk(m dump.ChunkMeta) (*dump.Chunk, error) {
	offset := m.Index * m.RowsLen
	limit := m.RowsLen
	query := "SELECT * FROM metrics"
	where := make([]string, 0, 3)
	if s.cfg.Where != "" {
		where = append(where, fmt.Sprintf("(%s)", s.cfg.Where))
	}
	if m.Start != nil {
		where = append(where, fmt.Sprintf("period_start > %d", m.Start.Unix()))
	}
	if m.End != nil {
		where = append(where, fmt.Sprintf("period_start < %d", m.End.Unix()))
	}
	for i := range where {
		if i == 0 {
			query += " WHERE "
		} else {
			query += " AND "
		}
		query += where[i]
	}
	query += fmt.Sprintf(" ORDER BY period_start, queryid LIMIT %d OFFSET %d", limit, offset)
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]interface{}, len(columns))
	for i := range columns {
		values[i] = new(interface{})
	}
	buf := new(bytes.Buffer)
	writer := tsv.NewWriter(buf)
	for rows.Next() {
		if err := rows.Scan(values...); err != nil {
			return nil, err
		}
		valuesStr := toStringSlice(values)
		if err := writer.Write(valuesStr); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	writer.Flush()
	if err = writer.Error(); err != nil {
		return nil, err
	}

	return &dump.Chunk{
		ChunkMeta: m,
		Content:   buf.Bytes(),
		Filename:  fmt.Sprintf("%d.tsv", m.Index),
	}, err
}

func toStringSlice(iSlice []interface{}) []string {
	values := make([]string, 0, cap(iSlice))
	for _, v := range iSlice {
		value := v.(*interface{})
		if value == nil {
			values = append(values, "")
			continue
		}
		values = append(values, fmt.Sprintf("%v", *value))
	}
	return values
}

func (s Source) WriteChunk(_ string, r io.Reader) error {
	reader := tsv.NewReader(r, s.ColumnTypes())

	for {
		records, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		_, err = s.stmt.Exec(records...)
		if err != nil {
			return err
		}
	}

	return nil
}

func prepareInsertStatement(tx *sql.Tx, columnsCount int) (*sql.Stmt, error) {
	var query strings.Builder

	query.Grow(28 + columnsCount*2)
	query.WriteString("INSERT INTO metrics VALUES (")
	for i := 0; i < columnsCount-1; i++ {
		query.WriteString("?,")
	}
	query.WriteString("?)")
	return tx.Prepare(query.String())
}

func (s Source) FinalizeWrites() error {
	if err := s.stmt.Close(); err != nil {
		return err
	}
	return s.tx.Commit()
}

func (s Source) Count(where string) (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM metrics"
	if where != "" {
		query += fmt.Sprintf(" WHERE %s", where)
	}
	row := s.db.QueryRow(query)
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s Source) ColumnTypes() []*sql.ColumnType {
	return s.ct
}

func (s Source) SplitIntoChunks(startTime, endTime time.Time, chunkRowsLen int) ([]dump.ChunkMeta, error) {
	if chunkRowsLen <= 0 {
		return nil, errors.Errorf("invalid chunk rows len: %v", chunkRowsLen)
	}

	totalRows, err := s.Count(s.cfg.Where)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get amount of ClickHouse records")
	}

	rowsCounter := totalRows
	chunksLen := rowsCounter/chunkRowsLen + 1
	chunks := make([]dump.ChunkMeta, 0, chunksLen)
	i := 0
	for rowsCounter > 0 {
		newChunk := dump.ChunkMeta{
			Source:  dump.ClickHouse,
			RowsLen: chunkRowsLen,
			Index:   i,
			Start:   &startTime,
			End:     &endTime,
		}
		chunks = append(chunks, newChunk)
		rowsCounter -= chunkRowsLen
		i++
	}

	log.Debug().
		Int("rows", totalRows).
		Int("chunk_size", chunkRowsLen).
		Int("chunks", len(chunks)).
		Msg("Split Click House rows into chunks")

	return chunks, nil
}
