package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

var errMaintenanceActive = errors.New("spark maintenance blocks new admission")

// MaintenanceReader lets the host control admission without granting the Worker write access.
type MaintenanceReader interface {
	Read() types.SparkMaintenanceState
}

func (h *Handler) maintenanceState() types.SparkMaintenanceState {
	if h.cfg.Maintenance == nil {
		return types.SparkMaintenanceState{Known: true}
	}
	return h.cfg.Maintenance.Read()
}

// RecoverNonTerminal keeps persisted work dormant across restarts during maintenance.
// Call once before subscribing. Status remains available while explicit resume is awaited.
func (h *Handler) RecoverNonTerminal(ctx context.Context) error {
	h.admissionBarrier.Lock()
	defer h.admissionBarrier.Unlock()
	h.stateMu.Lock()
	open := h.admissionOpen
	h.stateMu.Unlock()
	if !open {
		return errAdmissionClosed
	}
	// 重置只发生在订阅前，避免延迟恢复覆盖解除维护后已运行的记录。
	if err := h.runInbox.ResetProcessing(ctx, h.RunSubject()); err != nil {
		return fmt.Errorf("reset interrupted inbox records: %w", err)
	}
	state := h.maintenanceState()
	if state.Known && !state.Active {
		return h.recoverNonTerminalNow(ctx)
	}
	h.recoveryWG.Add(1)
	go h.waitMaintenanceRecovery()
	return nil
}

func (h *Handler) waitMaintenanceRecovery() {
	defer h.recoveryWG.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-h.execCtx.Done():
			return
		case <-h.admissionStopped:
			return
		case <-ticker.C:
		}
		h.admissionBarrier.Lock()
		state := h.maintenanceState()
		h.stateMu.Lock()
		open := h.admissionOpen
		h.stateMu.Unlock()
		if !open {
			h.admissionBarrier.Unlock()
			return
		}
		if state.Known && !state.Active {
			// Exclusive with admission: redeliveries after resume cannot acquire the same record twice.
			err := h.recoverNonTerminalNow(h.execCtx)
			if err != nil {
				h.recoveryFailed.Store(true)
			}
			h.admissionBarrier.Unlock()
			return
		}
		h.admissionBarrier.Unlock()
	}
}
