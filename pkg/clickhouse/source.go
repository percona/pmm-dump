package clickhouse

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"github.com/ClickHouse/clickhouse-go"
	"github.com/pkg/errors"
	"io"
	"pmm-transferer/pkg/dump"
)

type Source struct {
	db  *sql.DB
	cfg Config
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

	return &Source{
		cfg: cfg,
		db:  db,
	}, nil
}

func (s Source) Type() dump.SourceType {
	return dump.ClickHouse
}

func (s Source) ReadChunk(m dump.ChunkMeta) (*dump.Chunk, error) {
	offset := m.Index * m.RowsLen
	limit := m.RowsLen
	query := "SELECT * FROM metrics"
	if m.Where != "" {
		query += fmt.Sprintf(" WHERE %s", m.Where)
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
	writer := newTSVWriter(buf)
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

func (s Source) WriteChunk(_ string, _ io.Reader) error {
	// TODO
	return errors.New("not implemented")
}

func (s Source) FinalizeWrites() error {
	// TODO
	return errors.New("not implemented")
}

func (s Source) Count(where *string) (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM metrics"
	if where != nil {
		query += fmt.Sprintf(" WHERE %s", *where)
	}
	row := s.db.QueryRow(query)
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s Source) SplitIntoChunks(where *string) ([]dump.ChunkMeta, error) {
	rowsCount, err := s.Count(where)
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
		if where != nil {
			newChunk.Where = *where
		}
		chunks = append(chunks, newChunk)
		rowsCount -= chunkRowsLen
		i++
	}
	return chunks, nil
}

func newTSVWriter(w io.Writer) *csv.Writer {
	writer := csv.NewWriter(w)
	writer.Comma = '\t'
	return writer
}
