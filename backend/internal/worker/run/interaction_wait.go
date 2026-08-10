package run

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/ygpkg/yg-go/logs"
)

// computeSlotHandle 表示当前 run 持有的计算槽的所有权。
// runBatch / executeDirect 取得计算槽后交给 handle；交互等待时可释放/重取，结束幂等。
type computeSlotHandle struct {
	coordinator *Coordinator
	// recovered 表示该 run 来自崩溃恢复，占用一个恢复许可。
	recovered bool

	mu   sync.Mutex
	held bool
}

// release 将计算槽（含恢复许可）归还给池（幂等）。
func (h *computeSlotHandle) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held {
		<-h.coordinator.slots
		if h.recovered {
			<-h.coordinator.recoverySlots
		}
		h.held = false
	}
}

// acquire 重新获取一个计算槽（含恢复许可）；ctx 取消时返回错误。
func (h *computeSlotHandle) acquire(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held {
		return nil
	}
	if h.recovered {
		select {
		case h.coordinator.recoverySlots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case h.coordinator.slots <- struct{}{}:
		h.held = true
		return nil
	case <-ctx.Done():
		if h.recovered {
			<-h.coordinator.recoverySlots
		}
		return ctx.Err()
	}
}

// interactionWaiter 是 Coordinator 注入到 run ctx 的交互等待观察者实现。
// 负责：交互等待槽核算、计算槽释放/重取、超时 watchdog。
type interactionWaiter struct {
	coordinator *Coordinator
	runID       string
	compute     *computeSlotHandle
	// cancelRun 用于 watchdog 超时后取消整个 run。
	cancelRun context.CancelFunc
	// timedOut 记录 watchdog 是否已触发（用于准确记录结束状态为 timeout）。
	timedOut atomic.Bool
}

// BeginInteractionWait 实现 agent.InteractionWaitObserver。
func (w *interactionWaiter) BeginInteractionWait(
	ctx context.Context,
	requestID string,
	kind string,
) (func() error, error) {
	if w == nil || w.coordinator == nil {
		return nil, fmt.Errorf("interaction waiter is not initialized")
	}
	co := w.coordinator

	// 1. 申请交互等待槽——容量满则明确失败，不得继续占用计算槽。
	select {
	case co.interactionSlots <- struct{}{}:
	default:
		logs.WarnContextf(ctx, "interaction wait capacity full: worker.run.interaction.wait.full request_id=%s kind=%s run_id=%s active_waits=%d cap=%d",
			requestID, kind, w.runID, len(co.interactionSlots), cap(co.interactionSlots))
		return nil, fmt.Errorf("interaction wait capacity full (kind=%s)", kind)
	}

	// 2. 释放计算槽，进入交互等待。
	w.compute.release()
	start := time.Now()
	logs.InfoContextf(ctx, "interaction wait start: worker.run.interaction.wait.start request_id=%s kind=%s run_id=%s active_waits=%d cap=%d",
		requestID, kind, w.runID, len(co.interactionSlots), cap(co.interactionSlots))

	// 3. 注册交互等待记录（用于取消与计数）。
	wait := &interactionWait{runID: w.runID, started: start}
	co.waitsMu.Lock()
	co.waits[requestID] = wait
	co.waitsMu.Unlock()

	// releaseSlot 幂等释放交互等待记录与交互槽（watchdog 与 end() 共用）。
	releaseSlot := func() {
		co.waitsMu.Lock()
		delete(co.waits, requestID)
		co.waitsMu.Unlock()
		if !wait.ended.CompareAndSwap(false, true) {
			return
		}
		<-co.interactionSlots
	}

	// 4. 超时 watchdog：等待超过硬超时则取消任务并释放全部资源。
	timeoutTimer := time.AfterFunc(co.interactionWait, func() {
		w.timedOut.Store(true)
		logs.WarnContextf(ctx, "interaction wait timeout: worker.run.interaction.wait.timeout request_id=%s kind=%s run_id=%s timeout_ms=%d",
			requestID, kind, w.runID, int(co.interactionWait.Milliseconds()))
		releaseSlot()
		if w.cancelRun != nil {
			w.cancelRun()
		}
	})

	// end 显式记录结束状态并返回；调用方（Router）据此把 reacquire 失败作为最终错误返回。
	end := func() error {
		// 停止 watchdog，避免取消已结束的等待。
		timeoutTimer.Stop()
		releaseSlot()

		elapsed := time.Since(start).Milliseconds()
		// 重新获取计算槽。ctx 被取消（超时或外部取消）时不重取，返回错误结束任务。
		if err := w.compute.acquire(ctx); err != nil {
			if w.timedOut.Load() {
				logs.WarnContextf(ctx, "interaction wait ended(timeout): worker.run.interaction.wait.timeout request_id=%s kind=%s run_id=%s wait_ms=%d",
					requestID, kind, w.runID, elapsed)
			} else if errors.Is(err, context.Canceled) {
				logs.WarnContextf(ctx, "interaction wait ended(cancelled): worker.run.interaction.wait.cancelled request_id=%s kind=%s run_id=%s wait_ms=%d err=%v",
					requestID, kind, w.runID, elapsed, err)
			} else {
				logs.WarnContextf(ctx, "interaction wait reacquire_failed: worker.run.interaction.wait.reacquire_failed request_id=%s kind=%s run_id=%s wait_ms=%d err=%v",
					requestID, kind, w.runID, elapsed, err)
			}
			return err
		}
		logs.InfoContextf(ctx, "interaction wait resumed: worker.run.interaction.wait.resumed request_id=%s kind=%s run_id=%s wait_ms=%d",
			requestID, kind, w.runID, elapsed)
		return nil
	}
	return end, nil
}

// newInteractionWaiter 为一次 run 构建交互等待观察者，并注入到 ctx。
// compute 为 runBatch / executeDirect 已创建的计算槽 handle（与释放 defer 共享）。
// cancelRun 由调用方提供，用于 watchdog 超时后取消整个 run。
func (c *Coordinator) newInteractionWaiter(
	ctx context.Context,
	runID string,
	compute *computeSlotHandle,
	cancelRun context.CancelFunc,
) context.Context {
	observer := &interactionWaiter{
		coordinator: c,
		runID:       runID,
		compute:     compute,
		cancelRun:   cancelRun,
	}
	return agent.WithInteractionWaitObserver(ctx, observer)
}
