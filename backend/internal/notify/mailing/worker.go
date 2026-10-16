package mailing

import (
	"context"

	"github.com/riverqueue/river"

	"landlord/backend/internal/platform/jobs"
)

func dailyOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// DigestArgs — аргументы notify.owner_digest.
type DigestArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (DigestArgs) Kind() string { return DigestJobName }

// InsertOpts — уникальность по бизнес-дате.
func (DigestArgs) InsertOpts() river.InsertOpts { return dailyOpts() }

// RemindersArgs — аргументы notify.payment_reminders.
type RemindersArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (RemindersArgs) Kind() string { return RemindersJobName }

// InsertOpts — уникальность по бизнес-дате.
func (RemindersArgs) InsertOpts() river.InsertOpts { return dailyOpts() }

// ContractEndArgs — аргументы notify.contract_end.
type ContractEndArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (ContractEndArgs) Kind() string { return ContractEndName }

// InsertOpts — уникальность по бизнес-дате.
func (ContractEndArgs) InsertOpts() river.InsertOpts { return dailyOpts() }

// ReadingsArgs — аргументы notify.readings_reminder.
type ReadingsArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (ReadingsArgs) Kind() string { return ReadingsJobName }

// InsertOpts — уникальность по бизнес-дате.
func (ReadingsArgs) InsertOpts() river.InsertOpts { return dailyOpts() }

// Schedules — ежедневные рассылки в порядке 07 (08:00, 09:00, 09:05, 09:10 по Москве).
func Schedules() []jobs.Daily {
	return []jobs.Daily{
		{Name: DigestJobName, Hour: 8, Args: func(a jobs.DailyArgs) river.JobArgs { return DigestArgs{a} }},
		{Name: RemindersJobName, Hour: 9, Args: func(a jobs.DailyArgs) river.JobArgs { return RemindersArgs{a} }},
		{Name: ContractEndName, Hour: 9, Minute: 5, Args: func(a jobs.DailyArgs) river.JobArgs { return ContractEndArgs{a} }},
		{Name: ReadingsJobName, Hour: 9, Minute: 10, Args: func(a jobs.DailyArgs) river.JobArgs { return ReadingsArgs{a} }},
	}
}

// DigestWorker выполняет notify.owner_digest.
type DigestWorker struct {
	river.WorkerDefaults[DigestArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs.
func (w *DigestWorker) Work(ctx context.Context, job *river.Job[DigestArgs]) error {
	_, err := w.Svc.RunOwnerDigest(ctx, job.Args.DailyArgs)
	return err
}

// RemindersWorker выполняет notify.payment_reminders.
type RemindersWorker struct {
	river.WorkerDefaults[RemindersArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs.
func (w *RemindersWorker) Work(ctx context.Context, job *river.Job[RemindersArgs]) error {
	_, err := w.Svc.RunPaymentReminders(ctx, job.Args.DailyArgs)
	return err
}

// ContractEndWorker выполняет notify.contract_end.
type ContractEndWorker struct {
	river.WorkerDefaults[ContractEndArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs.
func (w *ContractEndWorker) Work(ctx context.Context, job *river.Job[ContractEndArgs]) error {
	_, err := w.Svc.RunContractEnd(ctx, job.Args.DailyArgs)
	return err
}

// ReadingsWorker выполняет notify.readings_reminder.
type ReadingsWorker struct {
	river.WorkerDefaults[ReadingsArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs.
func (w *ReadingsWorker) Work(ctx context.Context, job *river.Job[ReadingsArgs]) error {
	_, err := w.Svc.RunReadingsReminder(ctx, job.Args.DailyArgs)
	return err
}
