package mainpatch

// 修复后的 Sequencer 启动代码（自愈版本）
// 替换 cmd/indexer/main.go 第 320-352 行

	var wg sync.WaitGroup
	sm.fetcher.Start(ctx, &wg)
	fatalErrCh := make(chan error, 1)
	sequencer := engine.NewSequencerWithFetcher(sm.processor, sm.fetcher, startBlock, cfg.ChainID, sm.fetcher.Results, fatalErrCh, nil, engine.GetMetrics())

	wg.Add(1)
	slog.Info("⛓️ Engine Components Ignited", "start_block", startBlock.String())

	// 🚀 自愈 Sequencer：崩溃后自动重启
	go recovery.WithRecoveryNamed("sequencer_supervisor", func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// 启动 Sequencer（带自愈）
				slog.Info("🔄 [SELF-HEAL] Starting Sequencer...")
				recovery.WithRecoveryNamed("sequencer_run", func() { sequencer.Run(ctx) })

				// 如果 Sequencer 崩溃退出，等待 3 秒后重启
				slog.Warn("⚠️ [SELF-HEAL] Sequencer crashed, restarting in 3s...")
				select {
				case <-ctx.Done():
					return
				case <-time.After(3 * time.Second):
					slog.Info("♻️ [SELF-HEAL] Sequencer restarting...")
				}
			}
		}
	}()

	go recovery.WithRecoveryNamed("tail_follow", func() { sm.StartTailFollow(ctx, startBlock) })
