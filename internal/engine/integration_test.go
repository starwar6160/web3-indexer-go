//go:build integration

package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	testPostgresURL string
	testAnvilRPC    string
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// 1. 启动 Postgres 容器
	pgContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("web3_indexer_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		log.Fatalf("failed to start postgres container: %s", err)
	}

	// 2. 启动 Anvil 容器
	anvilContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "ghcr.io/foundry-rs/foundry:latest",
			ExposedPorts: []string{"8545/tcp"},
			Cmd:          []string{"anvil", "--host", "0.0.0.0"},
			WaitingFor:   wait.ForListeningPort("8545/tcp").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		if terr := pgContainer.Terminate(ctx); terr != nil {
			log.Printf("failed to terminate pg container: %v", terr)
		}
		log.Fatalf("failed to start anvil container: %s", err)
	}

	// 3. 提取连接字符串
	pgHost, err := pgContainer.Host(ctx)
	if err != nil {
		log.Fatalf("failed to get pg host: %v", err)
	}
	pgPort, err := pgContainer.MappedPort(ctx, "5432")
	if err != nil {
		log.Fatalf("failed to get pg port: %v", err)
	}
	testPostgresURL = fmt.Sprintf("postgres://postgres:password@%s:%s/web3_indexer_test?sslmode=disable", pgHost, pgPort.Port())

	anvilHost, err := anvilContainer.Host(ctx)
	if err != nil {
		log.Fatalf("failed to get anvil host: %v", err)
	}
	anvilPort, err := anvilContainer.MappedPort(ctx, "8545")
	if err != nil {
		log.Fatalf("failed to get anvil port: %v", err)
	}
	testAnvilRPC = fmt.Sprintf("http://%s:%s", anvilHost, anvilPort.Port())

	// 4. 注入环境变量供测试读取 (必须在 setupDatabase 之前，因为 setupDatabase 内部也可能依赖)
	if err := os.Setenv("DATABASE_URL", testPostgresURL); err != nil {
		log.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("RPC_URLS", testAnvilRPC); err != nil {
		log.Fatalf("failed to set env: %v", err)
	}

	// 5. 初始化数据库 Schema
	if err := setupDatabase(testPostgresURL); err != nil {
		if terr := pgContainer.Terminate(ctx); terr != nil {
			log.Printf("failed to terminate pg container: %v", terr)
		}
		if terr := anvilContainer.Terminate(ctx); terr != nil {
			log.Printf("failed to terminate anvil container: %v", terr)
		}
		log.Fatalf("failed to setup test database: %s", err)
	}

	// 6. 运行测试
	code := m.Run()

	// 🚀 给一丁点时间让异步清理和最后一次 I/O 完成，防止 connection reset
	time.Sleep(5 * time.Second)

	// 7. 优雅清理
	if terr := pgContainer.Terminate(ctx); terr != nil {
		log.Printf("failed to terminate pg container: %v", terr)
	}
	if terr := anvilContainer.Terminate(ctx); terr != nil {
		log.Printf("failed to terminate anvil container: %v", terr)
	}

	os.Exit(code)
}

func setupDatabase(dsn string) error {
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	// 🚀 从 migrations 目录顺序加载 Schema
	migrationFiles := []string{
		"../../migrations/001_init.sql",
		"../../migrations/002_visitor_stats.sql",
		"../../migrations/003_add_activity_type.sql",
		"../../migrations/004_token_metadata.sql",
	}

	for _, file := range migrationFiles {
		schema, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", file, err)
		}
		_, err = db.Exec(string(schema))
		if err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", file, err)
		}
	}

	return nil
}
