package clickhouse

import (
	"bytes"
	"database/sql"
	"fmt"
	"github.com/ClickHouse/clickhouse-go"
	"github.com/pkg/errors"
	"io"
	"pmm-transferer/pkg/clickhouse/tsv"
	"pmm-transferer/pkg/dump"
	"strings"
)

type Source struct {
	db  *sql.DB
	cfg Config
	tx  *sql.Tx
	ct  []*sql.ColumnType
}

const (
	chunkRowsLen = 1000
)

func NewSource(cfg Config) (*Source, error) {
	db, err := sql.Open("clickhouse", cfg.ConnectionURL)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
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
	return &Source{
		cfg: cfg,
		db:  db,
		tx:  tx,
		ct:  ct,
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
	query := fmt.Sprintf("SELECT * FROM metrics ORDER BY period_start, queryid LIMIT %d OFFSET %d", limit, offset)
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
	reader := tsv.NewReader(r)

	stmt, err := s.prepareStatement()
	if err != nil {
		return err
	}

	defer func(stmt *sql.Stmt) {
		err = stmt.Close()
	}(stmt)
	for {
		records, err := reader.Read(s.ColumnTypes())
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		_, err = stmt.Exec(records...)
		if err != nil {
			return err
		}
	}

	return err
}

func (s Source) prepareStatement() (*sql.Stmt, error) {
	valuesCount := len(s.ct)
	var query strings.Builder

	query.Grow(28 + valuesCount*2)
	query.WriteString("INSERT INTO metrics VALUES (")
	for i := 0; i < valuesCount-1; i++ {
		query.WriteString("?,")
	}
	query.WriteString("?)")
	return s.tx.Prepare(query.String())
}

func (s Source) FinalizeWrites() error {
	return s.tx.Commit()
}

func (s Source) Count() (int, error) {
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM metrics")
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s Source) ColumnTypes() []*sql.ColumnType {
	return s.ct
}

func (s Source) SplitIntoChunks() ([]dump.ChunkMeta, error) {
	rowsCount, err := s.Count()
	if err != nil {
		return nil, errors.New(fmt.Sprintf("failed to get amount of ClickHouse records: %s", err))
	}
	chunksLen := rowsCount/chunkRowsLen + 1
	chunks := make([]dump.ChunkMeta, 0, chunksLen)
	i := 0
	for rowsCount > 0 {
		newChunk := dump.ChunkMeta{
			Source:  dump.ClickHouse,
			RowsLen: chunkRowsLen,
			Index:   i,
		}
		chunks = append(chunks, newChunk)
		rowsCount -= chunkRowsLen
		i++
	}
	return chunks, nil
}
