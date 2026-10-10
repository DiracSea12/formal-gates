package facade

import (
	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/protocol"
)

// handoffSliceReceipt runs after the child checkpoint/publisher has released
// its store, owner and promotion locks. The master acceptance owns its run lock.
func (f *Facade) handoffSliceReceipt(receipt protocol.SliceReceipt) error {
	master, err := Open(f.root, receipt.Receipt.MasterRunID, f.runtimeFiles)
	if err != nil {
		return err
	}
	unlock, err := master.acquireWriteWindow()
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := master.engine.Load(); isMissingEngineState(err) {
		summary, e := master.readSummary()
		if e != nil {
			return e
		}
		if summary.Termination != nil && summary.Termination.ChildCreation != nil {
			return nil
		}
	}
	return master.acceptSliceReceipt(receipt)
}

func (f *Facade) acceptSliceReceipt(receipt protocol.SliceReceipt) error {
	unlock, err := coordination.AcquireRunWaiting(f.root, f.runID)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := f.store.Recover(); err != nil {
		return err
	}
	return f.engine.AcceptSliceReceipt(receipt, emptyFingerprint())
}

// discoverSliceReceipts reads only exact claims, never scans for new members.
// It does not hold the master run lock or acquire a child run lock while loading
// and publishing child evidence. All mutations use the same acceptance entry.
func (f *Facade) discoverSliceReceipts(state *protocol.State) error {
	if state.SliceChild != nil || state.Topology == nil {
		return nil
	}
	for _, sliceID := range state.Topology.Slices {
		childID := state.SliceChildren[sliceID]
		if childID == "" {
			continue
		}
		child, err := Open(f.root, childID, f.runtimeFiles)
		if err != nil {
			return err
		}
		snapshot, err := child.engine.Load()
		if isMissingEngineState(err) {
			if accepted, ok := state.SliceReceipts[childID]; ok && accepted.Receipt.Status == "SLICE_READY" && state.ResourcesCleaned() {
				continue
			}
			summary, e := child.readSummary()
			if e != nil {
				return e
			}
			snapshot.State = summary.Termination
		} else if err != nil {
			return err
		}
		receipt := snapshot.State.SliceReceipt
		if receipt == nil || !state.SliceReceiptCurrent(*receipt) {
			continue
		}
		if err := child.publishSliceReceipt(receipt); err != nil {
			return err
		}
		if err := f.acceptSliceReceipt(*receipt); err != nil {
			return err
		}
	}
	return nil
}
