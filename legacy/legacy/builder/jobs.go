package builder

// This file implements a sequential job runner for the compiler.
// Jobs run in dependency order, one at a time. No goroutines, no
// nondeterminism. Determinism all the way down.

import (
	"fmt"
	"time"
)

// Set to true to enable logging in the job runner. This may help to debug
// concurrency or performance issues.
const jobRunnerDebug = false

// compileJob is a single compiler job, comparable to a single Makefile target.
// It is used to orchestrate various compiler tasks that can be run in parallel
// but that have dependencies and thus have limitations in how they can be run.
type compileJob struct {
	description  string // description, only used for logging
	dependencies []*compileJob
	result       string // result (path)
	run          func(*compileJob) (err error)
	err          error         // error if finished
	duration     time.Duration // how long it took to run this job (only set after finishing)
}

// dummyCompileJob returns a new *compileJob that produces an output without
// doing anything. This can be useful where a *compileJob producing an output is
// expected but nothing needs to be done, for example for a load from a cache.
func dummyCompileJob(result string) *compileJob {
	return &compileJob{
		description: "<dummy>",
		result:      result,
	}
}

// runJobs runs the indicated job and all its dependencies sequentially.
// Dependencies are resolved depth-first. No goroutines, no nondeterminism.
func runJobs(job *compileJob, sema chan struct{}) error {
	// Linearize jobs in dependency order (depth-first).
	var jobs []*compileJob
	added := map[*compileJob]struct{}{}
	var addJobs func(*compileJob)
	addJobs = func(job *compileJob) {
		if _, ok := added[job]; ok {
			return
		}
		for _, dep := range job.dependencies {
			addJobs(dep)
		}
		jobs = append(jobs, job)
		added[job] = struct{}{}
	}
	addJobs(job)

	// Run each job sequentially in dependency order.
	start := time.Now()
	for _, j := range jobs {
		if j.run == nil {
			continue
		}
		if jobRunnerDebug {
			fmt.Println("## start:   ", j.description)
		}
		jobStart := time.Now()
		if err := j.run(j); err != nil {
			return err
		}
		j.duration = time.Since(jobStart)
		if jobRunnerDebug {
			fmt.Println("## finished:", j.description, "(time "+j.duration.String()+")")
		}
	}

	if jobRunnerDebug {
		fmt.Println("## total:   ", time.Since(start))
	}
	return nil
}

