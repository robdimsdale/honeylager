package honeylager

import (
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"code.cloudfoundry.org/lager"
	libhoney "github.com/honeycombio/libhoney-go"
)

const (
	metadataKeyID = "id"
)

type Sink struct {
	minLogLevel lager.LogLevel
	builder     *libhoney.Builder
}

// NewSink returns a new Sink
// Callers are expected to call Close() when they are done
// e.g. sink := NewSink(); defer sink.Close()
// Callers may also wish to track the responses with ReadResponses()
func NewSink(
	honeycombWriteKey string,
	honeycombDataset string,
	minLogLevel lager.LogLevel,
) *Sink {
	b := libhoney.NewBuilder()
	b.WriteKey = honeycombWriteKey
	b.Dataset = honeycombDataset

	b.AddDynamicField(
		"num_goroutines",
		func() interface{} {
			return runtime.NumGoroutine()
		},
	)

	b.AddDynamicField(
		"memory_allocation",
		func() interface{} {
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			return mem.Alloc
		},
	)

	return &Sink{
		minLogLevel: minLogLevel,
		builder:     b,
	}
}

func (sink *Sink) Close() {
	libhoney.Close()
}

func (sink *Sink) Log(logFormat lager.LogFormat) {
	if logFormat.LogLevel < sink.minLogLevel {
		return
	}

	ev := sink.builder.NewEvent()

	// 0 is current function
	// 1 is lager.Info, lager.Debug etc
	// 2 is the function that called lager.Info, lager.Debug etc
	functionOffset := 2
	if pc, _, _, ok := runtime.Caller(functionOffset); ok {
		funcName := runtime.FuncForPC(pc).Name()
		ev.AddField("function", funcName)
	}

	ev.Metadata = map[string]interface{}{
		metadataKeyID: rand.Intn(math.MaxInt32),
	}
	ev.AddField("lager_source", logFormat.Source)
	ev.AddField("lager_message", logFormat.Message)
	ev.AddField("lager_log_level_iota", logFormat.LogLevel)
	ev.AddField("lager_log_level", logLevelToString(logFormat.LogLevel))

	// namespace the 'session' value becauase it isn't particularly useful for
	// event-based observability, and therefore namespacing it makes it easier to
	// reason about (and ignore).
	if session, ok := logFormat.Data["session"]; ok {
		ev.AddField("lager_session", session)
		delete(logFormat.Data, "session")
	}

	ev.Add(logFormat.Data)

	// Override the event timestamp if the JSON blob has a valid time. If time
	// is missing or it doesn't parse correctly, the event will be sent with the
	// default time (Now())
	ts, err := parseLagerTimestamp(logFormat.Timestamp)
	if err != nil {
		ev.AddField("lager_timestamp_parse_error", err)
	} else {
		ev.Timestamp = ts
	}

	err = ev.Send()
	if err != nil {
		printError(err)
	}
}

func parseLagerTimestamp(ts string) (time.Time, error) {
	// Example: "1504804895.094333887"

	timeFloat, err := strconv.ParseFloat(ts, 9)
	if err != nil {
		return time.Time{}, err
	}

	seconds := int64(timeFloat)
	nanos := int64((timeFloat - float64(seconds)) * 1e9)

	return time.Unix(seconds, nanos), nil
}

func logLevelToString(logLevel lager.LogLevel) string {
	switch logLevel {
	case lager.DEBUG:
		return "DEBUG"
	case lager.INFO:
		return "INFO"
	case lager.ERROR:
		return "ERROR"
	case lager.FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ReadResponses is a blocking method that waits for responses from Honeycomb
// and prints whether the event emission succeeded or failed.
// Callers will likely want to execute this method in a goroutine due to its
// blocking, asynchronous, nature.
func ReadResponses() {
	for r := range libhoney.Responses() {
		if r.StatusCode < http.StatusOK || r.StatusCode >= http.StatusMultipleChoices {
			printError(fmt.Errorf(
				"bad status code: '%d', err: '%v', response body: '%s'",
				r.StatusCode, r.Err, r.Body,
			))
			continue
		}

		if r.Metadata == nil {
			printError(fmt.Errorf("metadata was nil"))
			continue
		}

		metadataMap, ok := r.Metadata.(map[string]interface{})
		if !ok {
			printError(fmt.Errorf(
				"metadata was not expected type map[string]interface{}, metadata: %+v",
				r.Metadata,
			))
			continue
		}

		fmt.Printf(
			"Successfully sent event %v to Honeycomb in %v\n",
			metadataMap[metadataKeyID],
			r.Duration,
		)
	}
}

func printError(err error) {
	fmt.Printf("honeylager error: %v\n", err)
}
