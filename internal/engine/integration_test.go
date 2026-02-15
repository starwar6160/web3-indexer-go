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
		_ = pgContainer.Terminate(ctx)
		log.Fatalf("failed to start anvil container: %s", err)
	}

	// 3. 提取连接字符串
	pgHost, _ := pgContainer.Host(ctx)
	pgPort, _ := pgContainer.MappedPort(ctx, "5432")
	testPostgresURL = fmt.Sprintf("postgres://postgres:password@%s:%s/web3_indexer_test?sslmode=disable", pgHost, pgPort.Port())

	anvilHost, _ := anvilContainer.Host(ctx)
	anvilPort, _ := anvilContainer.MappedPort(ctx, "8545")
	testAnvilRPC = fmt.Sprintf("http://%s:%s", anvilHost, anvilPort.Port())

	// 4. 注入环境变量供测试读取 (必须在 setupDatabase 之前，因为 setupDatabase 内部也可能依赖)
	_ = os.Setenv("DATABASE_URL", testPostgresURL)
	_ = os.Setenv("RPC_URLS", testAnvilRPC)

	// 5. 初始化数据库 Schema
	if err := setupDatabase(testPostgresURL); err != nil {
		_ = pgContainer.Terminate(ctx)
		_ = anvilContainer.Terminate(ctx)
		log.Fatalf("failed to setup test database: %s", err)
	}

	// 6. 运行测试
	code := m.Run()

	// 7. 优雅清理
	_ = pgContainer.Terminate(ctx)
	_ = anvilContainer.Terminate(ctx)

	os.Exit(code)
}

func setupDatabase(dsn string) error {
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	// 读取并执行核心初始化 SQL
	schema, err := os.ReadFile("../../scripts/init-db.sql")
	if err != nil {
		return fmt.Errorf("failed to read init-db.sql: %w", err)
	}
	_, err = db.Exec(string(schema))
	if err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	return nil
}
