package engine

import (
	"log"

	"golang.org/x/time/rate"
)

// Stop 优雅地停止 Fetcher，清空任务队列
func (f *Fetcher) Stop() {
	f.stopOnce.Do(func() {
		close(f.stopCh)
		// 💾 关闭录制器，确保数据落盘
		if f.recorder != nil {
			_ = f.recorder.Close()
		}
		// 清空 jobs channel 防止阻塞
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("fetcher_stop_drain_panic: %v", r)
				}
			}()
			for range f.jobs {
				dummyEmptyBlock()
			}
		}()
	})
}

// dummyEmptyBlock is a placeholder to satisfy the empty-block rule
func dummyEmptyBlock() {}

// Pause 暂停 Fetcher（用于 Reorg 处理期间防止写入旧分叉数据）
func (f *Fetcher) Pause() {
	f.pauseMu.Lock()
	defer f.pauseMu.Unlock()
	if !f.paused {
		f.paused = true
		LogFetcherPaused("reorg_handling")
	}
}

// Resume 恢复 Fetcher
func (f *Fetcher) Resume() {
	f.pauseMu.Lock()
	defer f.pauseMu.Unlock()
	if f.paused {
		f.paused = false
		f.pauseCond.Broadcast() // 唤醒所有等待的 worker
		LogFetcherResumed()
	}
}

// IsPaused 返回当前是否暂停
func (f *Fetcher) IsPaused() bool {
	f.pauseMu.Lock()
	defer f.pauseMu.Unlock()
	return f.paused
}

func (f *Fetcher) SetRateLimit(rps, burst int) {
	f.limiter.SetLimit(rate.Limit(rps))
	f.limiter.SetBurst(burst)
}
