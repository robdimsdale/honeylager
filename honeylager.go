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
// Callers may also wish to track the responses with go ReadResponses()
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

	ev.Metadata = map[string]interface{}{
		metadataKeyID: rand.Intn(math.MaxInt32),
	}
	ev.AddField("lager_source", logFormat.Source)
	ev.AddField("lager_message", logFormat.Message)
	ev.AddField("lager_log_level_iota", logFormat.LogLevel)
	ev.AddField("lager_log_level", logLevelToString(logFormat.LogLevel))

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
