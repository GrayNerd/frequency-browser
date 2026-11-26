package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DaemonStatus describes the rigctld daemon the app is talking to. It drives
// the status line in the Radio tab of the Settings window.
type DaemonStatus struct {
	Running bool   `json:"running"`
	Owned   bool   `json:"owned"` // started by this app, so this app may stop it
	Address string `json:"address"`
	PID     int    `json:"pid"`
	ExitErr string `json:"exitErr"`
	Log     string `json:"log"`
	Note    string `json:"note"`
}

const (
	// daemonStopGrace is how long SIGTERM gets before SIGKILL.
	daemonStopGrace = 2 * time.Second
	// daemonProbeTimeout is the connect timeout for the "is it loaded" test.
	daemonProbeTimeout = 300 * time.Millisecond
)

// rigCapsTimeout bounds the capabilities probe used to validate a model.
const rigCapsTimeout = 5 * time.Second

// daemonSettleDelay is how long a freshly started daemon must stay reachable
// before startDaemon reports success.
const daemonSettleDelay = 150 * time.Millisecond

// daemonStartTimeout bounds how long to wait for a freshly started daemon to
// accept connections. rigctld opens and initialises the rig before it binds its
// socket, and real hardware is slow to answer: an Icom IC-7610 takes about seven
// seconds over CI-V, so a short timeout would fail on a rig that is working
// perfectly well. It is a variable so tests can shorten it.
var daemonStartTimeout = 30 * time.Second

var (
	daemonMu      sync.Mutex
	daemonCmd     *exec.Cmd
	daemonPID     int
	daemonOwned   bool
	daemonExitErr string
	daemonLog     *outputBuffer
	daemonNote    string
	// daemonDone is closed by the reaper when the tracked process exits, so a
	// restart can wait for the old daemon to really be gone.
	daemonDone chan struct{}
	// daemonAddress is the endpoint of the daemon in use, so the status line
	// reports what is actually running rather than what the dialog last held.
	daemonAddress string
)

// outputBuffer keeps the tail of the daemon's output so a rejected command line
// ("Config parameter error: ...") can be shown verbatim in the dialog instead of
// an opaque "did not start".
type outputBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func newOutputBuffer(max int) *outputBuffer { return &outputBuffer{max: max} }

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = b.data[len(b.data)-b.max:]
	}
	return len(p), nil
}

func (b *outputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

// daemonArgs maps the Radio tab's settings onto rigctld's command line.
//
// Serial options are passed only when a serial port is chosen: models without a
// serial port (Dummy, NET rigctl, FLRig) reject them with "Config parameter
// error". An empty choice means "Default", so the option is omitted and the
// backend's own default applies.
func daemonArgs(s Settings) ([]string, error) {
	if s.RigModel <= 0 {
		return nil, errors.New("no rig selected")
	}

	args := []string{"-m", strconv.Itoa(s.RigModel)}
	var conf []string

	portType, portKnown := rigPortType(s.RigModel)
	// Backend kinds: a serial port, a network link (another rigctld), or no
	// port at all (Dummy and friends). An unknown backend is treated as serial
	// so a hamlib that does not report capabilities never blocks a real rig.
	network := portKnown && portType == "Network link"
	noPort := portKnown && portType == "None"

	if network {
		// A network backend reaches the rig through another rigctld, so its
		// -r argument is that daemon's address, not a device file. Given a
		// serial device it binds its own socket and then never answers a
		// command, which reaches the user as an endless "i/o timeout".
		if s.SerialPort != "" {
			return nil, fmt.Errorf("rig model %d uses the %s backend and takes no serial port; "+
				"enter the remote host:port instead, or choose the radio itself",
				s.RigModel, portType)
		}
		if s.RemoteAddress == "" {
			return nil, fmt.Errorf("rig model %d uses the %s backend; enter the remote daemon's "+
				"address as host:port, for example "+defaultRemoteAddress, s.RigModel, portType)
		}
		if err := validateHostPort(s.RemoteAddress); err != nil {
			return nil, err
		}
		args = append(args, "-r", s.RemoteAddress)
	}

	if !network && !noPort && s.SerialPort != "" {
		args = append(args, "-r", s.SerialPort)
		if s.BaudRate > 0 {
			args = append(args, "-s", strconv.Itoa(s.BaudRate))
		}
		if s.DataBits != "" {
			conf = append(conf, "data_bits="+s.DataBits)
		}
		if s.StopBits != "" {
			conf = append(conf, "stop_bits="+s.StopBits)
		}
		if s.Handshake != "" {
			conf = append(conf, "serial_handshake="+s.Handshake)
		}
		if v := controlLineValue(s.DTR); v != "" {
			conf = append(conf, "dtr_state="+v)
		}
		if v := controlLineValue(s.RTS); v != "" {
			conf = append(conf, "rts_state="+v)
		}
	}

	if len(conf) > 0 {
		args = append(args, "-C", strings.Join(conf, ","))
	}

	host, port := splitHostPort(s.RadioAddress)
	args = append(args, "-T", host, "-t", port)
	return args, nil
}

// rigPortType reports how the given rig model is connected, by asking hamlib for
// its capabilities ("rigctld -m <id> -u"). The second return value is false when
// the capabilities could not be read, so callers can choose to allow rather than
// block, and never second-guess hamlib.
func rigPortType(model int) (string, bool) {
	if model <= 0 {
		return "", false
	}
	bin, err := rigctldPath()
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), rigCapsTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-m", strconv.Itoa(model), "-u").Output()
	if err != nil {
		return "", false
	}
	portType := parsePortType(string(out))
	if portType == "" {
		return "", false
	}
	return portType, true
}

// controlLineValue turns the dialog's High/Low into hamlib's ON/OFF. A blank
// choice leaves the line unforced (hamlib's "Unset").
func controlLineValue(v string) string {
	switch v {
	case "High":
		return "ON"
	case "Low":
		return "OFF"
	default:
		return ""
	}
}

// validateHostPort checks that address is host:port with a numeric port in
// range. net.SplitHostPort only splits, so "host:notaport" would otherwise get
// through and fail much later, inside rigctld.
func validateHostPort(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return fmt.Errorf("remote address %q must be host:port, for example %s",
			address, defaultRemoteAddress)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("remote address %q must have a port number between 1 "+
			"and 65535, for example %s", address, defaultRemoteAddress)
	}
	return nil
}

// splitHostPort splits the stored rigctld endpoint into rigctld's -T and -t
// values, defaulting to the loopback endpoint when it is empty or malformed.
func splitHostPort(address string) (host, port string) {
	if address == "" {
		address = defaultRadioAddress
	}
	h, p, err := net.SplitHostPort(address)
	if err != nil || p == "" {
		return "127.0.0.1", "4532"
	}
	if h == "" || h == "localhost" {
		h = "127.0.0.1"
	}
	return h, p
}

// probeDaemon reports whether something is listening on the rigctld address.
//
// rigctld binds its socket before it opens the rig, so a successful connect is a
// reliable "a daemon is loaded" test even when the radio itself is off. It is
// deliberately a plain connect-and-close: a full command exchange here proved
// less reliable right after startup (rigctld spins up its per-connection handler
// asynchronously), and transient connection resets are absorbed by
// sendRigCommand's retry instead.
func (a *App) probeDaemon(address string) bool {
	if address == "" {
		address = defaultRadioAddress
	}
	conn, err := net.DialTimeout("tcp", address, daemonProbeTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ensureDaemon adopts a daemon that is already running, or starts one from the
// saved Radio settings. Called once, while the service starts.
func (a *App) ensureDaemon() error {
	s := a.currentSettings()

	if a.probeDaemon(s.RadioAddress) {
		setDaemonAddress(s.RadioAddress)
		setDaemonNote("Using the rigctld daemon already running at " + s.RadioAddress +
			". It was not started by this app, so it will be left running on exit.")
		log.Printf("rigctld: using the daemon already running at %s", s.RadioAddress)
		return nil
	}

	if s.RigModel <= 0 {
		return errors.New("no rig configured; choose one in Settings, Radio")
	}
	if err := a.startDaemon(s); err != nil {
		return err
	}
	log.Printf("rigctld: started model %d on %s", s.RigModel, s.RadioAddress)
	return nil
}

// startDaemon launches rigctld with the given settings and waits for its socket
// to accept connections, so the very next rig command succeeds.
func (a *App) startDaemon(s Settings) error {
	// Fail fast when the device is not there. hamlib would otherwise retry a
	// missing port indefinitely without ever binding its socket, which the
	// user would experience as a long freeze and then a vague timeout.
	if s.SerialPort != "" {
		// Only a serial backend opens a device file; a network backend's -r is
		// a host:port and must not be stat()ed.
		if portType, known := rigPortType(s.RigModel); !known || serialBackend(portType) {
			if _, err := os.Stat(s.SerialPort); err != nil {
				return fmt.Errorf("serial port %s is not available: %v", s.SerialPort, err)
			}
		}
	}

	args, err := daemonArgs(s)
	if err != nil {
		return err
	}
	bin, err := rigctldPath()
	if err != nil {
		return err
	}

	buf := newOutputBuffer(4096)
	cmd := exec.Command(bin, args...)
	cmd.Stdout = buf
	cmd.Stderr = buf
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start %s: %w", bin, err)
	}

	done := make(chan struct{})

	daemonMu.Lock()
	daemonCmd, daemonPID, daemonOwned = cmd, cmd.Process.Pid, true
	daemonExitErr, daemonLog, daemonNote = "", buf, ""
	daemonAddress, daemonDone = s.RadioAddress, done
	daemonMu.Unlock()

	// Reap the process so an early exit (bad model, rejected parameter) is
	// reported at once instead of surfacing as a connect timeout.
	go func() {
		defer close(done)
		waitErr := cmd.Wait()
		message := "exited unexpectedly"
		if waitErr != nil {
			message = waitErr.Error()
		}
		daemonMu.Lock()
		defer daemonMu.Unlock()
		// Always record why it exited: this is what the Settings window shows
		// when a command line is rejected. Only clear the tracked process if it
		// is still the one we are tracking (a replacement may already be
		// running, and its state must not be clobbered).
		daemonExitErr = message
		if daemonCmd == cmd {
			daemonCmd, daemonPID, daemonOwned = nil, 0, false
		}
	}()

	deadline := time.Now().Add(daemonStartTimeout)
	for time.Now().Before(deadline) {
		// Require our own process to be alive as well as the socket to answer:
		// something else may already hold the port (a second rigctld, or a
		// leftover), in which case the daemon we just started exits with
		// "address in use" and the answering socket is not ours.
		if a.probeDaemon(s.RadioAddress) && processAlive(cmd.Process.Pid) {
			// Confirm it is still up a moment later. rigctld can answer the
			// very first connect and then exit (for example when it loses the
			// race for the port to a daemon that is still shutting down), and
			// returning at that instant would hand the caller a dead daemon.
			time.Sleep(daemonSettleDelay)
			if a.probeDaemon(s.RadioAddress) && processAlive(cmd.Process.Pid) {
				return nil
			}
		}
		daemonMu.Lock()
		startupErr := daemonExitErr
		daemonMu.Unlock()
		if startupErr != "" {
			return fmt.Errorf("rigctld could not start (%s)%s", startupErr, outputTail(buf))
		}
		time.Sleep(50 * time.Millisecond)
	}

	a.stopDaemon()
	return fmt.Errorf("rigctld did not listen on %s within %s%s",
		s.RadioAddress, daemonStartTimeout, outputTail(buf))
}

// outputTail renders the daemon's captured output as a suffix for an error.
func outputTail(buf *outputBuffer) string {
	if buf == nil {
		return ""
	}
	if out := buf.String(); out != "" {
		return ": " + out
	}
	return ""
}

// stopDaemon terminates the daemon, but only when this app started it. A daemon
// that was already running when the app started is never touched.
func (a *App) stopDaemon() {
	daemonMu.Lock()
	cmd, pid, owned, done := daemonCmd, daemonPID, daemonOwned, daemonDone
	daemonMu.Unlock()
	if cmd == nil || !owned || cmd.Process == nil {
		return
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("rigctld: could not signal the daemon: %v", err)
	}

	// Wait for the process to be reaped rather than polling kill(pid, 0),
	// which also reports a zombie as alive.
	if done != nil && !waitClosed(done, daemonStopGrace) {
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("rigctld: could not kill the daemon: %v", err)
		}
		waitClosed(done, daemonStopGrace)
	}

	daemonMu.Lock()
	if daemonCmd == cmd {
		daemonCmd, daemonPID, daemonOwned, daemonDone = nil, 0, false, nil
	}
	daemonMu.Unlock()

	// Give the kernel a moment to release the listen socket, so an immediate
	// restart on the same port can bind it.
	deadline := time.Now().Add(daemonStopGrace)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitClosed waits for ch to be closed, reporting whether it happened in time.
func waitClosed(ch <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// processAlive reports whether pid still exists (signal 0 probes, it does not
// deliver anything).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// restartDaemon applies new Radio settings. The daemon this app owns is
// restarted; one that belongs to somebody else is left alone, with a note for
// the dialog, because killing it could disrupt another program using the rig.
func (a *App) restartDaemon(s Settings) error {
	if !a.ownsDaemon() && a.probeDaemon(s.RadioAddress) {
		setDaemonNote("A rigctld daemon started outside this app is in use, so it was not restarted. " +
			"Restart that daemon to apply these settings.")
		return nil
	}
	a.stopDaemon()
	return a.startDaemon(s)
}

func (a *App) ownsDaemon() bool {
	daemonMu.Lock()
	defer daemonMu.Unlock()
	return daemonOwned
}

func setDaemonNote(note string) {
	daemonMu.Lock()
	daemonNote = note
	daemonMu.Unlock()
}

func setDaemonAddress(address string) {
	daemonMu.Lock()
	daemonAddress = address
	daemonMu.Unlock()
}

// GetDaemonStatus reports the daemon state for the Settings window.
func (a *App) GetDaemonStatus() DaemonStatus {
	// The daemon's own endpoint is authoritative; the settings are only a
	// fallback for when nothing has been started or adopted yet.
	address := a.currentSettings().RadioAddress
	daemonMu.Lock()
	if daemonAddress != "" {
		address = daemonAddress
	}
	status := DaemonStatus{
		Address: address,
		PID:     daemonPID,
		Owned:   daemonOwned,
		ExitErr: daemonExitErr,
		Note:    daemonNote,
	}
	if daemonLog != nil {
		status.Log = daemonLog.String()
	}
	daemonMu.Unlock()

	status.Running = a.probeDaemon(status.Address)
	return status
}

// ServiceShutdown stops the daemon when this app started it, and does nothing
// to a daemon that was already running. Wails calls this for services that
// implemented ServiceStartup.
func (a *App) ServiceShutdown() error {
	a.stopDaemon()
	return nil
}
