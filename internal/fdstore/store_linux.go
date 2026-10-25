//go:build linux

// Package fdstore keeps explicitly admitted live kernel file descriptions in
// the worker epoch. It cannot recover them after that worker exits.
package fdstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const MaximumFiles = 128
const maximumMessage = 32 << 10

var keyPattern = regexp.MustCompile(`^[0-9a-f]{32}:[0-9a-f]{32}$`)
var namePattern = regexp.MustCompile(`^factorybox-(reg|dir|inotify|fanotify):[1-9][0-9]{0,9}$`)

type message struct {
	Operation   string      `json:"operation"`
	Key         string      `json:"key"`
	Names       []string    `json:"names"`
	Error       string      `json:"error,omitempty"`
	Directories []Directory `json:"directories,omitempty"`
}

type generation struct {
	names       []string
	files       []*os.File
	directories []Directory
}

type Server struct {
	listener    *net.UnixListener
	mutex       sync.Mutex
	generations map[string]generation
	closed      bool
	wait        sync.WaitGroup
}

func Start(path string) (*Server, error) {
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	server := &Server{listener: listener, generations: map[string]generation{}}
	server.wait.Add(1)
	go func() {
		defer server.wait.Done()
		for {
			connection, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			// Serial bounded requests keep the epoch's descriptor budget exact.
			server.serve(connection)
			connection.Close()
		}
	}()
	return server, nil
}

func closeFiles(files []*os.File) {
	for _, file := range files {
		file.Close()
	}
}

func (server *Server) Close() error {
	server.mutex.Lock()
	server.closed = true
	for key, generation := range server.generations {
		closeFiles(generation.files)
		delete(server.generations, key)
	}
	server.mutex.Unlock()
	err := server.listener.Close()
	server.wait.Wait()
	return err
}

func credentials(connection *net.UnixConn) (*unix.Ucred, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return nil, err
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	return credential, controlErr
}

func receive(connection *net.UnixConn) (message, []*os.File, error) {
	var decoded message
	data := make([]byte, maximumMessage)
	oob := make([]byte, unix.CmsgSpace(MaximumFiles*4))
	n, ancillary, flags, _, err := connection.ReadMsgUnix(data, oob)
	if err != nil {
		return decoded, nil, err
	}
	var files []*os.File
	messages, err := unix.ParseSocketControlMessage(oob[:ancillary])
	if err != nil {
		return decoded, nil, err
	}
	for _, control := range messages {
		rights, err := unix.ParseUnixRights(&control)
		if err != nil {
			closeFiles(files)
			return decoded, nil, err
		}
		for _, fd := range rights {
			unix.CloseOnExec(fd)
			files = append(files, os.NewFile(uintptr(fd), "epoch-retained file"))
		}
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || len(files) > MaximumFiles || json.Unmarshal(data[:n], &decoded) != nil {
		closeFiles(files)
		return decoded, nil, fmt.Errorf("invalid or truncated retained-file request")
	}
	return decoded, files, nil
}

func send(connection *net.UnixConn, value message, files []*os.File) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > maximumMessage || len(files) > MaximumFiles {
		return fmt.Errorf("retained-file response exceeds bounds")
	}
	var fds []int
	for _, file := range files {
		fds = append(fds, int(file.Fd()))
	}
	var oob []byte
	if len(fds) > 0 {
		oob = unix.UnixRights(fds...)
	}
	n, ancillary, err := connection.WriteMsgUnix(data, oob, nil)
	if err != nil {
		return err
	}
	if n != len(data) || ancillary != len(oob) {
		return fmt.Errorf("partial retained-file message")
	}
	return nil
}

func validNames(names []string) bool {
	if len(names) > MaximumFiles {
		return false
	}
	seen := map[string]bool{}
	for _, name := range names {
		if (!namePattern.MatchString(name) && name != SourceRootName) || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

func (server *Server) serve(connection *net.UnixConn) {
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	peer, err := credentials(connection)
	if err != nil || peer.Uid != 0 {
		return
	}
	request, files, err := receive(connection)
	if err != nil {
		return
	}
	defer func() { closeFiles(files) }()
	server.mutex.Lock()
	defer server.mutex.Unlock()
	reply := message{Operation: request.Operation, Key: request.Key}
	var result []*os.File
	if server.closed || !keyPattern.MatchString(request.Key) {
		reply.Error = "retained-file epoch closed or invalid identity"
	} else {
		saved, exists := server.generations[request.Key]
		switch request.Operation {
		case "ping":
			if len(files) != 0 || len(request.Names) != 0 {
				reply.Error = "invalid retained-file ping"
			}
		case "put":
			if exists || len(server.generations) >= 16 || len(files) != len(request.Names) || !validNames(request.Names) || validateDirectories(request.Names, files, request.Directories) != nil {
				reply.Error = "retained-file generation already exists or exceeds admission bounds"
			} else {
				server.generations[request.Key] = generation{names: request.Names, files: files, directories: request.Directories}
				files = nil
			}
		case "get":
			if len(files) != 0 || len(request.Names) != 0 || !exists {
				reply.Error = "retained-file generation is absent or request differs"
			} else {
				reply.Names, result = saved.names, saved.files
			}
		case "restore":
			if len(files) != 0 || len(request.Names) != 0 || len(request.Directories) != 0 || !exists {
				reply.Error = "retained-file generation is absent or request differs"
			} else {
				var err error
				reply.Names, err = restoreNames(saved)
				if err != nil {
					reply.Error = err.Error()
				} else {
					result = saved.files
				}
			}
		case "release":
			if len(files) != 0 || len(request.Names) != 0 {
				reply.Error = "invalid retained-file release"
			} else if exists {
				closeFiles(saved.files)
				delete(server.generations, request.Key)
			}
		default:
			reply.Error = "unknown retained-file operation"
		}
	}
	send(connection, reply, result)
}

func exchange(path string, request message, files []*os.File) (message, []*os.File, error) {
	var reply message
	connection, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		return reply, nil, err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	peer, err := credentials(connection)
	if err != nil || peer.Uid != 0 || peer.Pid != 1 {
		return reply, nil, fmt.Errorf("retained-file endpoint is not the original root worker PID 1")
	}
	if err := send(connection, request, files); err != nil {
		return reply, nil, err
	}
	reply, received, err := receive(connection)
	if err != nil {
		return reply, nil, err
	}
	if reply.Operation != request.Operation || reply.Key != request.Key || reply.Error != "" {
		closeFiles(received)
		return reply, nil, fmt.Errorf("retained-file operation unconfirmed: %s", reply.Error)
	}
	return reply, received, nil
}

func Put(path, key string, names []string, files []*os.File) error {
	return PutDirectories(path, key, names, files, nil)
}

func PutDirectories(path, key string, names []string, files []*os.File, directories []Directory) error {
	if !validNames(names) || len(names) != len(files) {
		return fmt.Errorf("invalid retained-file set")
	}
	if err := validateDirectories(names, files, directories); err != nil {
		return err
	}
	_, received, err := exchange(path, message{Operation: "put", Key: key, Names: append([]string{}, names...), Directories: directories}, files)
	closeFiles(received)
	return err
}

// VerifyRestore resolves renamed directories through the pinned canonical
// source root before a durable Resume attempt is admitted. CRIU repeats this
// resolution when it receives the descriptors.
func VerifyRestore(path, key string) error {
	_, files, err := exchange(path, message{Operation: "restore", Key: key}, nil)
	closeFiles(files)
	return err
}

func Get(path, key string) ([]string, []*os.File, error) {
	reply, files, err := exchange(path, message{Operation: "get", Key: key}, nil)
	if err == nil && (!validNames(reply.Names) || len(reply.Names) != len(files)) {
		closeFiles(files)
		return nil, nil, errors.New("retained-file generation response differs")
	}
	return reply.Names, files, err
}

func Release(path, key string) error {
	_, files, err := exchange(path, message{Operation: "release", Key: key}, nil)
	closeFiles(files)
	return err
}

func Ping(path string) error {
	_, files, err := exchange(path, message{Operation: "ping", Key: "00000000000000000000000000000000:00000000000000000000000000000000"}, nil)
	closeFiles(files)
	return err
}
