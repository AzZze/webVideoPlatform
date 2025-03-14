package data

import (
	"fmt"
	"gorm.io/driver/mysql"
	"log/slog"
	"strings"

	"github.com/glebarez/sqlite"
	"github.com/google/wire"
	"github.com/gowvp/gb28181/internal/conf"
	"github.com/ixugo/goweb/pkg/orm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(SetupDB)

// SetupDB 初始化数据存储
func SetupDB(c *conf.Bootstrap, l *slog.Logger) (*gorm.DB, error) {
	cfg := c.Data.Database
	l.Info("initializing database", "dsn", cfg.Dsn)

	dial, isSQLite := getDialector(cfg.Dsn)
	if dial == nil {
		return nil, fmt.Errorf("invalid dialector for DSN: %s", cfg.Dsn)
	}

	// 为 SQLite 设置合理的默认值，但允许用户覆盖
	if isSQLite {
		if cfg.MaxIdleConns == 0 {
			cfg.MaxIdleConns = 1
		}
		if cfg.MaxOpenConns == 0 {
			cfg.MaxOpenConns = 1 // 单连接，或根据需要调整
		}
	}

	db, err := orm.New(true, dial, orm.Config{
		MaxIdleConns:    int(cfg.MaxIdleConns),
		MaxOpenConns:    int(cfg.MaxOpenConns),
		ConnMaxLifetime: cfg.ConnMaxLifetime.Duration(),
		SlowThreshold:   cfg.SlowThreshold.Duration(),
	}, orm.NewLogger(l, c.Debug, cfg.SlowThreshold.Duration()))
	if err != nil {
		l.Error("failed to initialize database", "error", err)
		return nil, fmt.Errorf("database init failed: %w", err)
	}

	// 为 SQLite 启用 WAL 模式（可选）
	if isSQLite {
		if err := db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
			l.Warn("failed to enable WAL mode", "error", err)
		}
	}

	l.Info("database initialized successfully")
	return db, nil
}

// getDialector 返回 dial 和 是否 sqlite
func getDialector(dsn string) (gorm.Dialector, bool) {
	if strings.Contains(dsn, "3306") {
		return mysql.Open(dsn), false
	}
	if strings.HasPrefix(dsn, "postgres") {
		return postgres.New(postgres.Config{
			DriverName: "pgx",
			DSN:        dsn,
		}), false
	}
	return sqlite.Open("E:\\goWorkSpace\\gb28181\\configs\\data.db"), true
}
