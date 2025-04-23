// Copyright 2023 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package deployment

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const pingTimeout = time.Second * 10

func (pmm *PMM) PingMongo(ctx context.Context) error {
	pmm.Log("Mongo URL:", pmm.MongoURL())
	cl, err := mongo.Connect(ctx, options.Client().ApplyURI(pmm.MongoURL()))
	if err != nil {
		return errors.Wrap(err, "failed to connect")
	}
	defer cl.Disconnect(ctx) //nolint:errcheck

	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	if err := cl.Ping(ctx, nil); err != nil {
		return errors.Wrap(err, "failed to ping")
	}
	return nil
}

func (pmm *PMM) PingClickhouse(ctx context.Context) error {
	pmm.Log("ClickHouse URL:", pmm.ClickhouseURL())
	db := clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{pmm.ClickhouseURL()},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "",
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout:          time.Second * 30,
		Debug:                true,
		BlockBufferSize:      10,
		MaxCompressionBuffer: 10240,
	})
	//db, err := sql.Open("clickhouse", pmm.ClickhouseURL())
	// if err != nil {
	// 	return err
	// }
	defer db.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		var exception *clickhouse.Exception
		if errors.As(err, &exception) {
			return errors.Errorf("exception: [%d] %s %s", exception.Code, exception.Message, exception.StackTrace)
		}
		return err
	}

	var count int
	query := "SELECT COUNT(*) FROM pmm.metrics"
	row := db.QueryRowContext(ctx, query)
	if err := row.Scan(&count); err != nil {
		fmt.Print(err)
	}
	return nil
}
