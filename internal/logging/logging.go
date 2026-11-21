package logging

import (
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// LoadBalancedTCPWriter implements a writer that load balances between multiple TCP connections
type LoadBalancedTCPWriter struct {
	addresses       []string
	conns           []*net.TCPConn
	current         uint64
	mutex           sync.RWMutex
	lastResolveTime time.Time
	resolveInterval time.Duration
	maxRetries      int
	retryDelay      time.Duration
	hostname        string
	port            string
}

// NewLoadBalancedTCPWriter creates a new load balanced writer that knows the original hostname and port
func NewLoadBalancedTCPWriter(addresses []string, hostname, port string) *LoadBalancedTCPWriter {
	writer := &LoadBalancedTCPWriter{
		addresses:       addresses,
		conns:           make([]*net.TCPConn, len(addresses)),
		resolveInterval: 5 * time.Minute, // Re-resolve DNS every 5 minutes
		maxRetries:      3,
		retryDelay:      time.Second,
		hostname:        hostname, // keep canonical hostname for future DNS refreshes
		port:            port,
	}
	writer.initConnections()
	return writer
}

// initConnections establishes initial connections to all endpoints
func (w *LoadBalancedTCPWriter) initConnections() {
	for i, addr := range w.addresses {
		conn, err := w.createConnection(addr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to connect to Logstash at %s: %v\n", addr, err)
			w.conns[i] = nil
		} else {
			w.conns[i] = conn
			fmt.Fprintf(os.Stderr, "Successfully connected to Logstash at %s\n", addr)
		}
	}
}

// createConnection creates a new TCP connection with timeout
func (w *LoadBalancedTCPWriter) createConnection(address string) (*net.TCPConn, error) {
	// Set connection timeout
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	conn, err := dialer.Dial("tcp", address)
	if err != nil {
		return nil, err
	}

	tcpConn := conn.(*net.TCPConn)

	// Configure timeouts and keepalive
	tcpConn.SetKeepAlive(true)
	tcpConn.SetKeepAlivePeriod(30 * time.Second)

	return tcpConn, nil
}

// Write implements io.Writer using round-robin between active connections
// Reconnection only happens when write fails
func (w *LoadBalancedTCPWriter) Write(p []byte) (n int, err error) {
	// Check if we need to refresh DNS resolution
	w.checkAndRefreshAddresses()

	w.mutex.RLock()
	defer w.mutex.RUnlock()

	if len(w.conns) == 0 {
		return 0, fmt.Errorf("no connections configured")
	}

	// Try to write to connections with round-robin
	startIndex := atomic.AddUint64(&w.current, 1) % uint64(len(w.conns))

	// First pass: try existing connections
	for i := 0; i < len(w.conns); i++ {
		index := (startIndex + uint64(i)) % uint64(len(w.conns))
		conn := w.conns[index]

		if conn == nil {
			continue
		}

		// Try to write with timeout
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Write(p)

		if err != nil {
			fmt.Fprintf(os.Stderr, "Write failed to %s: %v - closing connection\n", w.addresses[index], err)
			// Close failed connection and mark as nil
			conn.Close()
			w.conns[index] = nil
			continue
		}

		return n, nil
	}

	// Second pass: try to reconnect and write to failed connections
	fmt.Fprintf(os.Stderr, "All connections failed, attempting reconnection\n")

	for i := 0; i < len(w.conns); i++ {
		index := (startIndex + uint64(i)) % uint64(len(w.conns))

		if w.conns[index] != nil {
			continue // Already tried this one
		}

		// Try to reconnect
		fmt.Fprintf(os.Stderr, "Attempting reconnection to %s\n", w.addresses[index])
		conn, err := w.createConnectionWithRetry(w.addresses[index])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Reconnection failed to %s: %v\n", w.addresses[index], err)
			continue
		}

		// Update connection and try to write
		w.conns[index] = conn
		fmt.Fprintf(os.Stderr, "Reconnected to %s, attempting write\n", w.addresses[index])

		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Write(p)

		if err != nil {
			fmt.Fprintf(os.Stderr, "Write failed after reconnection to %s: %v\n", w.addresses[index], err)
			conn.Close()
			w.conns[index] = nil
			continue
		}

		return n, nil
	}

	// If we get here, check if we need to refresh DNS and try once more
	if w.shouldForceRefresh() {
		fmt.Fprintf(os.Stderr, "Forcing DNS refresh due to all connections failing\n")
		if err := w.forceRefreshConnections(); err != nil {
			return 0, fmt.Errorf("all connections failed and DNS refresh failed: %w", err)
		}

		// Try once more with refreshed connections. Reconnect nil entries first if needed
		for i := 0; i < len(w.conns); i++ {
			index := (startIndex + uint64(i)) % uint64(len(w.conns))
			conn := w.conns[index]

			if conn == nil {
				fmt.Fprintf(os.Stderr, "Attempting reconnection after DNS refresh to %s\n", w.addresses[index])
				var err error
				conn, err = w.createConnectionWithRetry(w.addresses[index])
				if err != nil {
					fmt.Fprintf(os.Stderr, "Reconnection after DNS refresh failed to %s: %v\n", w.addresses[index], err)
					continue
				}
				w.conns[index] = conn
			}

			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			n, err := conn.Write(p)

			if err != nil {
				conn.Close()
				w.conns[index] = nil
				continue
			}

			return n, nil
		}
	}

	return 0, fmt.Errorf("failed to write to any Logstash instance after reconnection attempts")
}

// createConnectionWithRetry creates a new TCP connection with retries
func (w *LoadBalancedTCPWriter) createConnectionWithRetry(address string) (*net.TCPConn, error) {
	var lastErr error

	for attempt := 0; attempt < w.maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * w.retryDelay
			time.Sleep(delay)
		}

		conn, err := w.createConnection(address)
		if err == nil {
			return conn, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("failed to connect after %d attempts: %w", w.maxRetries, lastErr)
}

// checkAndRefreshAddresses checks if DNS resolution should be refreshed
func (w *LoadBalancedTCPWriter) checkAndRefreshAddresses() {
	if w.hostname == "" || w.port == "" {
		return
	}

	if time.Since(w.lastResolveTime) < w.resolveInterval { // use time.Since
		return
	}

	// Try to refresh in background without blocking writes
	go func() {
		w.mutex.Lock()
		defer w.mutex.Unlock()

		// Double-check after acquiring lock
		if time.Since(w.lastResolveTime) < w.resolveInterval { // use time.Since
			return
		}

		w.refreshConnectionsLocked()
	}()
}

// shouldForceRefresh determines if we should force a DNS refresh
func (w *LoadBalancedTCPWriter) shouldForceRefresh() bool {
	if w.hostname == "" || w.port == "" {
		return false
	}

	// If all connections are nil, force immediate refresh
	allNil := true
	for _, c := range w.conns {
		if c != nil {
			allNil = false
			break
		}
	}
	if allNil {
		return true
	}

	// Force refresh if it's been more than 1 minute since last resolve
	// or if we've never resolved
	return time.Since(w.lastResolveTime) > time.Minute
}

// forceRefreshConnections forces a DNS refresh and reconnection (called with read lock)
func (w *LoadBalancedTCPWriter) forceRefreshConnections() error {
	w.mutex.RUnlock()
	w.mutex.Lock()
	defer func() {
		w.mutex.Unlock()
		w.mutex.RLock()
	}()

	return w.refreshConnectionsLocked()
}

// refreshConnectionsLocked refreshes connections (must be called with write lock)
func (w *LoadBalancedTCPWriter) refreshConnectionsLocked() error {

	newAddresses, err := ResolveLogstashAddresses(w.hostname, w.port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to refresh DNS resolution: %v\n", err)
		// Do not update lastResolveTime on error to allow quick retries when writes continue
		return err
	}

	// Check if addresses changed (order-insensitive)
	if !addressesEqual(w.addresses, newAddresses) {
		fmt.Fprintf(os.Stderr, "DNS resolution changed from %v to %v\n", w.addresses, newAddresses)

		// Close old connections
		for i, conn := range w.conns {
			if conn != nil {
				conn.Close()
				w.conns[i] = nil
			}
		}

		// Update addresses and create new connections
		w.addresses = newAddresses
		w.conns = make([]*net.TCPConn, len(newAddresses))
		w.initConnections()
	}

	w.lastResolveTime = time.Now()
	return nil
}

// addressesEqual compares two address slices (order-insensitive)
func addressesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		if m[s] == 0 {
			return false
		}
		m[s]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

// Close closes all connections
func (w *LoadBalancedTCPWriter) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	fmt.Fprintf(os.Stderr, "Closing all Logstash connections\n")

	for i, conn := range w.conns {
		if conn != nil {
			conn.Close()
			w.conns[i] = nil
		}
	}
	return nil
}

// ResolveLogstashAddresses resolves the headless service and returns IPs with port
func ResolveLogstashAddresses(hostname, port string) ([]string, error) {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve hostname %s: %w", hostname, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs found for hostname %s", hostname)
	}

	addresses := make([]string, 0, len(ips))
	for _, ip := range ips {
		// Only use IPv4
		if ip.To4() != nil {
			addresses = append(addresses, fmt.Sprintf("%s:%s", ip.String(), port))
		}
	}

	if len(addresses) == 0 {
		return nil, fmt.Errorf("no IPv4 addresses found for hostname %s", hostname)
	}

	return addresses, nil
}

// LogstashEncoder wraps a zapcore.Encoder and adds custom fields for Logstash
type LogstashEncoder struct {
	zapcore.Encoder
	appName    string
	appVersion string
	hostname   string
	revision   string
}

// NewLogstashEncoder creates a new encoder that adds application context from Viper
func NewLogstashEncoder(baseEncoder zapcore.Encoder) *LogstashEncoder {
	return &LogstashEncoder{
		Encoder:    baseEncoder,
		appName:    "slashsre-devops-controller",
		appVersion: viper.GetString("version"),
		hostname:   viper.GetString("hostname"),
		revision:   viper.GetString("revision"),
	}
}

// ecsError marshals an error as an ECS-compatible object ({"message": ..., "stack_trace": ...}).
// The Elasticsearch index maps "error" as an object, so sending it as a plain string
// makes Elasticsearch reject the whole document (document_parsing_exception).
type ecsError struct {
	message    string
	stackTrace string
}

// MarshalLogObject implements zapcore.ObjectMarshaler
func (e ecsError) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("message", e.message)
	if e.stackTrace != "" {
		enc.AddString("stack_trace", e.stackTrace)
	}
	return nil
}

// toECSErrorFields rewrites "error" fields (zap.Error / zap.String("error", ...)) into ECS error objects.
// It returns a new slice and never modifies the caller's slice.
func toECSErrorFields(fields []zapcore.Field) []zapcore.Field {
	out := make([]zapcore.Field, 0, len(fields)+4)
	for _, f := range fields {
		if f.Key != "error" {
			out = append(out, f)
			continue
		}
		switch f.Type {
		case zapcore.ErrorType:
			err, ok := f.Interface.(error)
			if !ok || err == nil {
				out = append(out, f)
				continue
			}
			e := ecsError{message: err.Error()}
			// Mirror zap's "errorVerbose" behaviour (e.g. pkg/errors stack traces)
			if _, isFormatter := err.(fmt.Formatter); isFormatter {
				if verbose := fmt.Sprintf("%+v", err); verbose != e.message {
					e.stackTrace = verbose
				}
			}
			out = append(out, zap.Object("error", e))
		case zapcore.StringType:
			out = append(out, zap.Object("error", ecsError{message: f.String}))
		default:
			out = append(out, f)
		}
	}
	return out
}

// EncodeEntry adds custom fields before encoding
func (enc *LogstashEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	// Add custom fields for Logstash
	customFields := append(toECSErrorFields(fields),
		zap.String("artifactId", enc.appName),
		zap.String("app_version", enc.appVersion),
		zap.String("hostname", enc.hostname),
		zap.String("revision", enc.revision),
	)

	return enc.Encoder.EncodeEntry(entry, customFields)
}

// Clone creates a copy of the encoder
func (enc *LogstashEncoder) Clone() zapcore.Encoder {
	return &LogstashEncoder{
		Encoder:    enc.Encoder.Clone(),
		appName:    enc.appName,
		appVersion: enc.appVersion,
		hostname:   enc.hostname,
		revision:   enc.revision,
	}
}

// InitLogger initializes a zap logger with console and optional Logstash output.
// The stopCh parameter should be an existing channel that will be closed when the application shuts down.
// Application context (name, version, hostname, revision) is automatically read from Viper configuration
// and added only to Logstash logs.
func InitLogger(logLevel string, logstashHost, logstashPort string, stopCh <-chan struct{}) (*zap.Logger, error) {
	level := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	switch logLevel {
	case "debug":
		level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case "info":
		level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "warn":
		level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "error":
		level = zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	case "fatal":
		level = zap.NewAtomicLevelAt(zapcore.FatalLevel)
	case "panic":
		level = zap.NewAtomicLevelAt(zapcore.PanicLevel)
	}

	zapEncoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Console encoder (without additional context)
	consoleEncoder := zapcore.NewJSONEncoder(zapEncoderConfig)
	consoleDebugging := zapcore.Lock(os.Stderr)
	cores := []zapcore.Core{zapcore.NewCore(consoleEncoder, consoleDebugging, level)}

	if logstashHost != "" && logstashPort != "" {
		logstashAddresses, err := ResolveLogstashAddresses(logstashHost, logstashPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Logstash logging disabled - %v\n", err)
		} else if len(logstashAddresses) > 0 {
			loadBalancedWriter := NewLoadBalancedTCPWriter(logstashAddresses, logstashHost, logstashPort)
			tcpSync := zapcore.AddSync(loadBalancedWriter)

			// Logstash encoder (with additional context)
			logstashBaseEncoder := zapcore.NewJSONEncoder(zapEncoderConfig)
			logstashEncoder := NewLogstashEncoder(logstashBaseEncoder)

			cores = append(cores, zapcore.NewCore(logstashEncoder, tcpSync, level))

			// Only periodic DNS refresh, no health checks
			go func() {
				ticker := time.NewTicker(5 * time.Minute)
				defer ticker.Stop()
				defer loadBalancedWriter.Close()

				for {
					select {
					case <-ticker.C:
						// Just trigger a DNS check, no health checks
						loadBalancedWriter.checkAndRefreshAddresses()
					case <-stopCh:
						return
					}
				}
			}()
		}
	}

	return zap.New(zapcore.NewTee(cores...), zap.AddCaller()), nil
}

// InitLoggerWithFields is a simpler alternative that uses global fields
// This option adds the fields to ALL logs (console and Logstash)
func InitLoggerWithFields(logLevel string, logstashHost, logstashPort string, stopCh <-chan struct{}) (*zap.Logger, error) {
	logger, err := InitLogger(logLevel, logstashHost, logstashPort, stopCh)
	if err != nil {
		return nil, err
	}

	// Crear un logger con campos predefinidos usando valores de Viper
	return logger.With(
		zap.String("app_name", "slashsre-devops-controller"),
		zap.String("app_version", viper.GetString("version")),
		zap.String("hostname", viper.GetString("hostname")),
		zap.String("revision", viper.GetString("revision")),
	), nil
}
