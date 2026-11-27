package mobilevoice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	pb "notes-bot/proto/whisper"
)

const (
	pollAfter  = 5 * time.Second
	retryAfter = 20 * time.Second
)

const (
	invalidRecording = "Не удалось прочитать запись"
	emptyRecording   = "В записи не распознана речь"
	transcriptFailed = "Не удалось расшифровать запись"
	noteUnavailable  = "Не удалось дописать расшифровку в выбранную заметку"
)

type queue interface {
	Claim(context.Context) (*Job, error)
	IsFront(context.Context, *Job) (bool, error)
	Release(context.Context, *Job, time.Duration) error
}

type transcriber interface {
	SubmitOnce(context.Context, io.Reader, string, string, string) (string, int, error)
	GetStatus(context.Context, string) (*clients.JobResult, error)
	Ack(context.Context, string) (bool, error)
}

type noteWriter interface {
	clients.IdempotentNoteService
	clients.IdempotentNoteByPathService
	EnsureNote(context.Context, string) (bool, error)
}

type Worker struct {
	store   queue
	whisper transcriber
	notes   noteWriter
	logger  *zap.Logger
}

func NewWorker(store queue, whisper transcriber, notes noteWriter, logger *zap.Logger) *Worker {
	return &Worker{store: store, whisper: whisper, notes: notes, logger: logger}
}

func (w *Worker) Run(ctx context.Context) {
	wait := time.NewTimer(0)
	defer wait.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wait.C:
		}
		if ctx.Err() != nil {
			return
		}
		wait.Reset(w.step(ctx))
	}
}

// Persist one checkpoint per claim. A replacement process resumes from the
// last checkpoint and uses the same operation identity for both external calls.
func (w *Worker) step(ctx context.Context) time.Duration {
	claimCtx, cancelClaim := context.WithTimeout(ctx, 5*time.Second)
	job, err := w.store.Claim(claimCtx)
	cancelClaim()
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Warn("failed to claim voice job")
		}
		return retryAfter
	}
	if job == nil {
		return time.Second
	}

	workCtx, cancelWork := context.WithTimeout(ctx, 125*time.Second)
	delay := w.advance(workCtx, job)
	cancelWork()

	// Release promptly on shutdown too. The lease still fences late writes.
	saveCtx, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelSave()
	if err := w.store.Release(saveCtx, job, delay); err != nil && !errors.Is(err, errLeaseLost) {
		w.logger.Warn("failed to checkpoint voice job", zap.String("job_id", job.ID))
		return retryAfter
	}
	return 0
}

func (w *Worker) advance(ctx context.Context, job *Job) time.Duration {
	if job.State == Queued {
		return w.submit(ctx, job)
	}
	if !job.ResultReady {
		return w.poll(ctx, job)
	}
	return w.deliver(ctx, job)
}

func (w *Worker) submit(ctx context.Context, job *Job) time.Duration {
	format, identity := "mp4", "notes-mobile:"+job.ID
	if job.web {
		format, identity = WebAudioFormat(job.Audio), "notes-web:"+job.ID
		if format == "" {
			job.State, job.Error = Failed, invalidRecording
			return 0
		}
	}
	remoteID, _, err := w.whisper.SubmitOnce(ctx, bytes.NewReader(job.Audio), format, "voice", identity)
	switch {
	case status.Code(err) == codes.InvalidArgument:
		job.State, job.Error = Failed, invalidRecording
	case err != nil || remoteID == "":
		return retryAfter
	default:
		job.RemoteID, job.State = remoteID, Transcribing
	}
	return 0
}

func (w *Worker) poll(ctx context.Context, job *Job) time.Duration {
	result, err := w.whisper.GetStatus(ctx, job.RemoteID)
	if err != nil || result == nil {
		return retryAfter
	}
	switch result.Status {
	case pb.JobStatus_FAILED:
		job.Error = transcriptFailed
	case pb.JobStatus_DONE:
		job.Transcript = strings.TrimSpace(result.Text)
		if job.Transcript == "" {
			job.Error = emptyRecording
		}
	default:
		return pollAfter
	}
	job.ResultReady = true
	return 0
}

func (w *Worker) deliver(ctx context.Context, job *Job) time.Duration {
	if job.Error == "" {
		front, err := w.store.IsFront(ctx, job)
		if err != nil {
			return retryAfter
		}
		if !front {
			return pollAfter
		}
	}
	// Only acknowledge after the result is in Notes PostgreSQL. A previous Ack
	// may have succeeded, and Transcriber may already have deleted the result.
	ack, err := w.whisper.Ack(ctx, job.RemoteID)
	if status.Code(err) != codes.NotFound && (err != nil || !ack) {
		return retryAfter
	}
	if job.Error != "" {
		job.State = Failed
		return 0
	}

	stored, err := w.append(ctx, job)
	switch status.Code(err) {
	case codes.NotFound, codes.InvalidArgument, codes.PermissionDenied, codes.FailedPrecondition, codes.AlreadyExists:
		job.State, job.Error = Failed, noteUnavailable
		return 0
	}
	if err != nil || !stored {
		return retryAfter
	}
	job.State, job.Transcript = Saved, ""
	return 0
}

func (w *Worker) append(ctx context.Context, job *Job) (bool, error) {
	operationID := "mobile-voice:" + job.ID
	if job.web {
		operationID = "web-voice:" + job.ID
	}
	if job.UploadPath != "" {
		return w.notes.AppendToNoteByPathOnce(ctx, job.UploadPath, job.Transcript, operationID)
	}
	if ensured, err := w.notes.EnsureNote(ctx, job.UploadDate); err != nil || !ensured {
		return false, err
	}
	return w.notes.AppendToNoteOnce(ctx, job.UploadDate, job.Transcript, operationID)
}
