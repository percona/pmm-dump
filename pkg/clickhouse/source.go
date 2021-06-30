package clickhouse

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"github.com/ClickHouse/clickhouse-go"
	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
	"io"
	"pmm-transferer/pkg/dump"
	"time"
)

type Source struct {
	c   *fasthttp.Client
	db  *sql.DB
	cfg Config
}

func NewSource(c *fasthttp.Client, cfg Config) (*Source, error) {
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
		c:   c,
		cfg: cfg,
		db:  db,
	}, nil
}

func (s Source) Type() dump.SourceType {
	return dump.ClickHouse
}

func (s Source) ReadChunk(m dump.ChunkMeta) (*dump.Chunk, error) {
	sleepTime := m.Start.Sub(time.Now())
	time.Sleep(sleepTime)

	offset := m.Index * m.RowsLen
	limit := m.RowsLen
	rows, err := s.db.Query(fmt.Sprintf("SELECT * FROM metrics ORDER BY period_start, queryid LIMIT %d OFFSET %d", limit, offset))
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err = rows.Close()
	}(rows)

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]interface{}, len(columns))
	valuePtr := make([]interface{}, len(columns))
	for i := range columns {
		valuePtr[i] = &values[i]
	}
	buf := new(bytes.Buffer)
	writer := newTSVWriter(buf)
	if err := writer.Write(columns); err != nil {
		return nil, err
	}

	for rows.Next() {
		if err := rows.Scan(valuePtr...); err != nil {
			return nil, err
		}
		valuesStr := make([]string, 0, len(columns))
		for _, v := range values {
			if v == nil {
				valuesStr = append(valuesStr, "")
			}
			valuesStr = append(valuesStr, fmt.Sprintf("%v", v))
		}
		if err := writer.Write(valuesStr); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	content, err := io.ReadAll(buf)
	if err != nil {
		return nil, err
	}

	return &dump.Chunk{
		ChunkMeta: m,
		Content:   content,
		Filename:  fmt.Sprintf("clickhouse-%d-%d.tsv", m.Index, m.Start.Unix()),
	}, err
}

func (s Source) WriteChunk(_ string, _ io.Reader) error {
	// TODO
	return errors.New("not implemented")
}

func (s Source) FinalizeWrites() error {
	// TODO
	return errors.New("not implemented")
}

func (s Source) Close() error {
	return s.db.Close()
}

func (s Source) Count() (int, error) {
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM metrics")
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func CreateChunks(start time.Time, delay time.Duration, rowsCount, chunkRowsLen int) ([]dump.ChunkMeta, error) {
	chunksLen := rowsCount/chunkRowsLen + 1
	chunks := make([]dump.ChunkMeta, 0, chunksLen)
	i := 0
	for rowsCount > 0 {
		newTime := start
		newChunk := dump.ChunkMeta{
			Source:  dump.ClickHouse,
			Start:   &newTime,
			RowsLen: chunkRowsLen,
			Index:   i,
		}
		chunks = append(chunks, newChunk)
		rowsCount -= chunkRowsLen
		start = start.Add(delay)
		i++
	}
	return chunks, nil
}

func newTSVWriter(w io.Writer) *csv.Writer {
	writer := csv.NewWriter(w)
	writer.Comma = '\t'
	return writer
}
