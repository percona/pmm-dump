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
	"database/sql"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const pingTimeout = time.Second * 5

func (pmm *PMM) PingMongo(ctx context.Context) error {
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
	db, err := sql.Open("clickhouse", pmm.ClickhouseURL())
	if err != nil {
		return err
	}
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
	return nil
}
