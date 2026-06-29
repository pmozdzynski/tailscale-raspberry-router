package handlers

// setupProgressReporter streams bootstrap step status to the setup wizard (SSE).
// status: running | ok | error | done
type setupProgressReporter func(status, step, detail string)

func (fn setupProgressReporter) running(step, detail string) {
	if fn != nil {
		fn("running", step, detail)
	}
}

func (fn setupProgressReporter) ok(step, detail string) {
	if fn != nil {
		fn("ok", step, detail)
	}
}

func (fn setupProgressReporter) fail(step, detail string) {
	if fn != nil {
		fn("error", step, detail)
	}
}
