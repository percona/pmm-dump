package deployment

import (
	"context"
	"database/sql"
	"time"

	"github.com/ClickHouse/clickhouse-go"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func (pmm *PMM) PingMongo(ctx context.Context) error {
	cl, err := mongo.Connect(ctx, options.Client().ApplyURI(pmm.MongoURL()))
	if err != nil {
		return errors.Wrap(err, "failed to connect")
	}
	defer cl.Disconnect(ctx)
	if err := cl.Ping(ctx, nil); err != nil {
		return errors.Wrap(err, "failed to ping")
	}
	return nil
}

func (pmm *PMM) PingClickhouse(ctx context.Context) error {
	db, err := sql.Open("clickhouse", pmm.ClickhouseURL())
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		var exception *clickhouse.Exception
		if errors.As(err, &exception) {
			return errors.Errorf("exception: [%d] %s %s", exception.Code, exception.Message, exception.StackTrace)
		}
		return err
	}
	return nil
}
