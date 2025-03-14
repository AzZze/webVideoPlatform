package proxydb

import (
	"context"
	"testing"

	"github.com/ixugo/goweb/pkg/orm"
	"wvp/internal/core/proxy"
)

func TestStreamProxyGet(t *testing.T) {
	db, mock, err := generateMockDB()
	if err != nil {
		t.Fatal(err)
	}
	userDB := NewStreamProxy(db)

	mock.ExpectQuery(`SELECT \* FROM "stream_proxys" WHERE id=\$1 (.+) LIMIT \$2`).WithArgs("jack", 1)
	var out proxy.StreamProxy
	if err := userDB.Get(context.Background(), &out, orm.Where("id=?", "jack")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal("ExpectationsWereMet err:", err)
	}
}
