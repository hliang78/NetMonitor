package NetMonitor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPingTarget(t *testing.T) {

	type arg struct {
		ip       string
		timeout  int
		count    int
		interval int
		want     string
		err      error
	}

	var args = []arg{
		{
			"202.96.209.5", 1000, 1, 2000, "Hello Test", nil,
		},
		// {
		// 	"10.10.3.1", 1000, 1, 2000, "Hello Test", nil,
		// },
	}

	for _, arg := range args {
		got, err := PingTarget(arg.ip, arg.timeout, arg.count, arg.interval)
		assert.IsType(t, arg.err, err)
		assert.Equal(t, arg.want, got)
		// if got != arg.want || err != arg.err {
		// 	t.Fatalf("Expected %s and %s, got %s and %s, ", got, err, arg.want, arg.err)
		// }
	}

}
