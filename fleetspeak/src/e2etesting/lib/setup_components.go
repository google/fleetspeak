package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/golang/protobuf/proto"
	ptypes "github.com/golang/protobuf/ptypes"
	duration "github.com/golang/protobuf/ptypes/duration"
	daemonservice_pb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	client_config_pb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	spb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
	fcpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
	grpcservice_pb "github.com/google/fleetspeak/fleetspeak/src/server/grpcservice/proto/fleetspeak_grpcservice"
	services_pb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	"google.golang.org/grpc"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"sync"
	"syscall"
	"time"
)

var (
	mysql_database = flag.String("mysql_database", "", "MySQL database name to use")
	mysql_username = flag.String("mysql_username", "", "MySQL username to use")
	mysql_password = flag.String("mysql_password", "", "MySQL password to use")
)

type startedProcessesPids struct {
	server_pid  chan int
	client_pid  chan int
	service_pid chan int
}

func (sp *startedProcessesPids) init() {
	sp.server_pid = make(chan int, 1)
	sp.client_pid = make(chan int, 1)
	sp.service_pid = make(chan int, 1)
}

func killProcess(pid int) {
	syscall.Kill(pid, syscall.SIGTERM)
	for {
		process, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		err = process.Signal(syscall.Signal(0))
	}
}

func (sp *startedProcessesPids) killAll() {
	pids := make([]int, 0)
	close(sp.server_pid)
	close(sp.client_pid)
	close(sp.service_pid)
	for pid := range sp.server_pid {
		pids = append(pids, pid)
	}
	for pid := range sp.client_pid {
		pids = append(pids, pid)
	}
	for pid := range sp.service_pid {
		pids = append(pids, pid)
	}

	for _, pid := range pids {
		syscall.Kill(pid, syscall.SIGTERM)
	}
	for {
		finished := true
		for _, pid := range pids {
			process, err := os.FindProcess(pid)
			if err != nil {
				continue
			}
			err = process.Signal(syscall.Signal(0))
			if err == nil {
				finished = false
				break
			} else {
				fmt.Println(pid, err)
			}
		}
		if finished {
			break
		}
		time.Sleep(time.Second)
	}
}

// Starts a command and redirects its output to main stdout
func startProcess(cmd *exec.Cmd, pid_chan chan int) {
	CopyAndCapture := func(w io.Writer, r io.Reader) ([]byte, error) {
		var out []byte
		buf := make([]byte, 1024, 1024)
		for {
			n, err := r.Read(buf[:])
			if n > 0 {
				d := buf[:n]
				out = append(out, d...)
				_, err := w.Write(d)
				if err != nil {
					return out, err
				}
			}
			if err != nil {
				// Read returns io.EOF at the end of file, which is not an error for us
				if err == io.EOF {
					err = nil
				}
				return out, err
			}
		}
	}

	stdoutIn, _ := cmd.StdoutPipe()
	stderrIn, _ := cmd.StderrPipe()
	err := cmd.Start()
	if err != nil {
		fmt.Println("cmd.Start() failed: " + err.Error())
	}
	pid_chan <- cmd.Process.Pid

	// cmd.Wait() should be called only after we finish reading
	// from stdoutIn and stderrIn.
	// wg ensures that we finish
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		CopyAndCapture(os.Stdout, stdoutIn)
		wg.Done()
	}()

	CopyAndCapture(os.Stderr, stderrIn)
	wg.Wait()
	cmd.Wait()
}

func getNewClientId(admin_port int, start_time time.Time) string {
	admin_addr := fmt.Sprintf("localhost:%v", admin_port)
	conn, err := grpc.Dial(admin_addr, grpc.WithInsecure())
	if err != nil {
		fmt.Printf("Unable to connect to fleetspeak admin interface [%v]: %v", admin_addr, err)
	}
	admin := services_pb.NewAdminClient(conn)
	ctx := context.Background()
	var ids [][]byte
	for i := 1; i <= 10; i++ {
		if i > 1 {
			time.Sleep(time.Second * 1)
		}
		res, err := admin.ListClients(ctx,
			&services_pb.ListClientsRequest{ClientIds: ids},
			grpc.MaxCallRecvMsgSize(1024*1024*1024))
		if err != nil {
			fmt.Printf("ListClients RPC failed: %v", err)
			continue
		}
		if len(res.Clients) == 0 {
			fmt.Printf("No new clients yet")
			continue
		}
		client := res.Clients[0]
		last_visit_time, _ := ptypes.Timestamp(client.LastContactTime)
		for _, cl := range res.Clients {
			cur_last_visit_time, _ := ptypes.Timestamp(cl.LastContactTime)
			if cur_last_visit_time.After(last_visit_time) {
				client = cl
				last_visit_time = cur_last_visit_time
			}
		}

		id, err := common.BytesToClientID(client.ClientId)
		if err != nil {
			fmt.Printf("Ignoring invalid client id [%v], %v", client.ClientId, err)
			continue
		}
		ts, err := ptypes.Timestamp(client.LastContactTime)
		if ts.After(start_time) {
			return fmt.Sprintf("%v", id)
		}
	}
	return ""
}

func configureFleetspeak(temp_path string, fs_frontend_port, fs_admin_port int) {
	var config cpb.Config
	config.ConfigurationName = "FleetspeakSetup"

	config.ComponentsConfig = new(fcpb.Config)
	config.ComponentsConfig.MysqlDataSourceName =
		fmt.Sprintf("%v:%v@tcp(127.0.0.1:3306)/%v", *mysql_username, *mysql_password, *mysql_database)

	config.ComponentsConfig.HttpsConfig = new(fcpb.HttpsConfig)
	config.ComponentsConfig.HttpsConfig.ListenAddress = fmt.Sprintf("localhost:6060")
	config.ComponentsConfig.HttpsConfig.DisableStreaming = true

	config.ComponentsConfig.AdminConfig = new(fcpb.AdminConfig)
	config.ComponentsConfig.AdminConfig.ListenAddress = fmt.Sprintf("localhost:%v", fs_admin_port)

	config.PublicHostPort =
		append(config.PublicHostPort, config.ComponentsConfig.HttpsConfig.ListenAddress)

	config.ServerComponentConfigurationFile = path.Join(temp_path, "server.config")
	config.TrustedCertFile = path.Join(temp_path, "trusted_cert.pem")
	config.TrustedCertKeyFile = path.Join(temp_path, "trusted_cert_key.pem")
	config.ServerCertFile = path.Join(temp_path, "server_cert.pem")
	config.ServerCertKeyFile = path.Join(temp_path, "server_cert_key.pem")
	config.LinuxClientConfigurationFile = path.Join(temp_path, "linux_client.config")
	config.WindowsClientConfigurationFile = path.Join(temp_path, "windows_client.config")
	config.DarwinClientConfigurationFile = path.Join(temp_path, "darwin_client.config")

	built_configurator_config_path := path.Join(temp_path, "configurator.config")
	ioutil.WriteFile(built_configurator_config_path, []byte(proto.MarshalTextString(&config)), 0644)

	// Build fleetspeak configurations
	_, err := exec.Command("config", "-config", built_configurator_config_path).Output()
	if err != nil {
		fmt.Println("error: " + err.Error())
	}

	// Adjust client config
	var client_config client_config_pb.Config
	client_config_data, err := ioutil.ReadFile(config.LinuxClientConfigurationFile)
	proto.UnmarshalText(string(client_config_data), &client_config)
	client_config.GetFilesystemHandler().ConfigurationDirectory = temp_path
	client_config.GetFilesystemHandler().StateFile = path.Join(temp_path, "client.state")
	os.Create(client_config.GetFilesystemHandler().StateFile)
	ioutil.WriteFile(config.LinuxClientConfigurationFile, []byte(proto.MarshalTextString(&client_config)), 0644)

	// Client services configuration
	client_service_conf := spb.ClientServiceConfig{Name: "FRR_client", Factory: "Daemon"}
	var payload daemonservice_pb.Config
	payload.Argv = append(payload.Argv, "python", "../../../../frr_python/frr_client.py")
	client_service_conf.Config, _ = ptypes.MarshalAny(&payload)
	os.Mkdir(path.Join(temp_path, "textservices"), 0777)
	os.Mkdir(path.Join(temp_path, "services"), 0777)
	os.Create(path.Join(temp_path, "communicator.txt"))
	ioutil.WriteFile(path.Join(temp_path, "textservices", "frr.textproto"), []byte(proto.MarshalTextString(&client_service_conf)), 0644)

	// Server services configuration
	server_service_conf := services_pb.ServiceConfig{Name: "FRR_server", Factory: "GRPC"}
	grpc_config := grpcservice_pb.Config{Target: fmt.Sprintf("localhost:%v", fs_frontend_port), Insecure: true}
	server_service_conf.Config, _ = ptypes.MarshalAny(&grpc_config)
	server_conf := services_pb.ServerConfig{Services: []*services_pb.ServiceConfig{&server_service_conf}, BroadcastPollTime: new(duration.Duration)}
	server_conf.BroadcastPollTime.Seconds = 1
	built_server_services_config_path := path.Join(temp_path, "server.services.config")
	ioutil.WriteFile(built_server_services_config_path, []byte(proto.MarshalTextString(&server_conf)), 0644)
}

func startProcesses(started_processes *startedProcessesPids, temp_path string, fs_frontend_port, fs_admin_port int) {
	// Start server
	server_run_cmd := exec.Command("server", "-logtostderr", "-components_config", path.Join(temp_path, "server.config"), "-services_config", path.Join(temp_path, "server.services.config"))
	go startProcess(server_run_cmd, started_processes.server_pid)

	server_start_time := time.Now()

	// Sleep and start client
	time.Sleep(time.Second * 1)
	client_run_cmd := exec.Command("client", "-logtostderr", "-config", path.Join(temp_path, "linux_client.config"))
	go startProcess(client_run_cmd, started_processes.client_pid)

	// Get new client's id and start service in current process
	client_id := getNewClientId(fs_admin_port, server_start_time)
	fmt.Println("Enrolled client_id: ", client_id)
	service_run_cmd := exec.Command(
		"python",
		"../../../../frr_python/frr_server.py",
		fmt.Sprintf("--client_id=%v", client_id),
		fmt.Sprintf("--fleetspeak_message_listen_address=localhost:%v", fs_frontend_port),
		fmt.Sprintf("--fleetspeak_server=localhost:%v", fs_admin_port))
	startProcess(service_run_cmd, started_processes.service_pid)
}

func main() {
	flag.Parse()

	temp_path, _ := ioutil.TempDir(os.TempDir(), "*_fleetspeak")
	fs_frontend_port := 6062
	fs_admin_port := 6061

	configureFleetspeak(temp_path, fs_frontend_port, fs_admin_port)

	var started_processes startedProcessesPids
	started_processes.init()
	defer started_processes.killAll()

	startProcesses(&started_processes, temp_path, fs_frontend_port, fs_admin_port)
}
