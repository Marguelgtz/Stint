package local

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestSSHWrapperBypassesMultiplexingForForwardOnlyTunnel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix SSH wrapper is not used on Windows")
	}
	wrapper, err := SSHExecutable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	forwardCase := strings.Index(script, `*" -N "*)`)
	multiplex := strings.Index(script, "ControlMaster=auto")
	if forwardCase < 0 {
		t.Fatal("SSH wrapper does not special-case -N forwarding tunnels")
	}
	if multiplex < 0 {
		t.Fatal("SSH wrapper no longer multiplexes short lifecycle commands")
	}
	if forwardCase > multiplex {
		t.Fatal("-N forwarding case must be handled before multiplexed SSH path")
	}
}
