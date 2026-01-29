package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/noahxzhu/pushover-notify/internal/model"
	"github.com/noahxzhu/pushover-notify/internal/pushover"
	"github.com/noahxzhu/pushover-notify/internal/storage"
)

type Worker struct {
	store      *storage.Store
	client     *pushover.Client
	updateChan chan struct{}
}

func NewWorker(store *storage.Store) *Worker {
	return &Worker{
		store:      store,
		client:     &pushover.Client{},
		updateChan: make(chan struct{}, 1),
	}
}

// Refresh signals the worker to re-evaluate the schedule immediately
func (w *Worker) Refresh() {
	select {
	case w.updateChan <- struct{}{}:
	default:
		// Channel already has a pending signal, no need to block
	}
}

func (w *Worker) Start(ctx context.Context) {
	slog.Info("Worker started (Event-Driven)")

	timer := time.NewTimer(time.Hour) // Initial long duration
	timer.Stop()                      // Stop immediately, we'll reset it

	for {
		// 1. Process due items and calculate next run time
		nextRun := w.checkAndProcess()

		// 2. Set timer
		now := time.Now()
		var duration time.Duration

		if nextRun.IsZero() {
			// No pending items, wait indefinitely (via updateChan)
			// effectively stop timer
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			slog.Info("No pending notifications. Worker idle.")
		} else {
			duration = nextRun.Sub(now)
			if duration < 0 {
				duration = 0 // Run immediately
			}

			// Reset timer
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(duration)
			slog.Info("Next check scheduled", "in", duration, "at", nextRun.Format("15:04:05"))
		}

		// 3. Wait for event
		select {
		case <-ctx.Done():
			slog.Info("Worker stopped")
			return
		case <-w.updateChan:
			slog.Info("Worker received update signal. Refreshing...")
			// Continue loop -> re-check
		case <-timer.C:
			// Timer fired -> Continue loop -> re-check
		}
	}
}

// checkAndProcess sends due notifications and returns the time of the NEXT scheduled event
func (w *Worker) checkAndProcess() time.Time {
	settings := w.store.GetSettings()

	// If credentials missing, we can't send, but we still need to wait?
	// If missing, we might as well idle until settings update?
	if settings.PushoverToken == "" || settings.PushoverUser == "" {
		return time.Time{} // Return zero to idle
	}

	w.client.Token = settings.PushoverToken
	w.client.User = settings.PushoverUser

	retryInterval, err := time.ParseDuration(settings.RetryInterval)
	if err != nil {
		retryInterval = 30 * time.Minute
	}

	maxRetries := settings.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	pending := w.store.GetPending()
	now := time.Now()
	saveNeeded := false

	var earliestNext time.Time

	for _, n := range pending {
		// Calculate when this notification SHOULD be sent next
		var nextSendTime time.Time

		if n.SendsCount == 0 {
			nextSendTime = n.ScheduledTime
		} else {
			nextSendTime = n.LastPushTime.Add(retryInterval)
		}

		// Check if it's due now (or past due)
		if !now.Before(nextSendTime) {
			// IT IS DUE
			if n.SendsCount < maxRetries {
				slog.Info("Sending notification", "content", n.Content, "attempt", n.SendsCount+1, "max", maxRetries)
				err := w.client.SendMessage("Reminder", n.Content)
				if err != nil {
					slog.Error("Failed to send pushover message", "error", err)
					// On failure, when to retry?
					// For simplicty, try again in 1 minute? Or stick to interval?
					// Let's stick to interval or try next tick?
					// If we retried immediately it would spam.
					// Let's assume we maintain the interval.
					// BUT, if we failed, we haven't updated LastPushTime.
					// So next loop (immediate) it would try again. infinite loop of failures?
					// To avoid this, we should probably update LastPushTime even on failure OR add a separate "LastTryTime".
					// For this project, let's update LastPushTime so we don't spam.
					n.LastPushTime = now
					saveNeeded = true
				} else {
					n.SendsCount++
					n.LastPushTime = now
					saveNeeded = true
				}
			}

			if n.SendsCount >= maxRetries {
				n.Status = model.StatusDone
				saveNeeded = true
				slog.Info("Notification marked as Done", "id", n.ID)
			} else {
				// Calculate NEXT time for this item after processing
				nextForThis := n.LastPushTime.Add(retryInterval)
				if earliestNext.IsZero() || nextForThis.Before(earliestNext) {
					earliestNext = nextForThis
				}
			}
		} else {
			// Not due yet, track its next time
			if earliestNext.IsZero() || nextSendTime.Before(earliestNext) {
				earliestNext = nextSendTime
			}
		}
	}

	if saveNeeded {
		if err := w.store.Save(); err != nil {
			slog.Error("Failed to save store", "error", err)
		}
	}

	return earliestNext
}
