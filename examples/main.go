package main

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"time"

	"code.cloudfoundry.org/lager"

	"github.com/robdimsdale/honeylager"
)

func main() {
	honeycombWriteKey := os.Getenv("HONEYCOMB_WRITE_KEY")

	sink := honeylager.NewSink(
		honeycombWriteKey,
		"honeycomb-golang-example",
		lager.DEBUG,
	)
	defer sink.Close()

	l := lager.NewLogger("my-component")
	l.RegisterSink(sink)
	go honeylager.ReadResponses()

	l.Info("example-starting")
	for i := 0; i < 10; i++ {
		duration := rand.Float64()*100 + 100
		payloadLength := rand.Intn(45)*50 + 5
		l.Debug("some-action", lager.Data{
			"duration_ms":    duration,
			"method":         "get",
			"hostname":       "appserver15",
			"payload_length": payloadLength,
		})

		time.Sleep(100 * time.Millisecond)
	}

	l.Error("example-error", errors.New("This is an example error"))

	l.Info("example-complete")

	time.Sleep(500 * time.Millisecond)
	fmt.Println("complete")
}
