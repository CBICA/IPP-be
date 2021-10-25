package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dgruber/drmaa2interface"
	"github.com/dgruber/drmaa2os"
	_ "github.com/dgruber/drmaa2os/pkg/jobtracker/dockertracker"
	"github.com/dgruber/drmaa2os/pkg/jobtracker/libdrmaa"
	"github.com/elliotchance/sshtunnel"
	"github.com/go-resty/resty/v2"
	"github.com/mholt/archiver/v3"
	"golang.org/x/crypto/ssh"
)

var (
	OUTPUT_DIR  = "outputs"
	INPUT_DIR   = "inputs"                    // note this must match what's hardcoded in the API server
	SSH_ADDR    = "root@host.docker.internal" // ssh login for machine the API server's running on
	REMOTE_ADDR = "localhost:5000"            // address API server is running on (when ssh'd in)
	LOCAL_PORT  = "0"                         // make the remote address available through any (random) local port
)

type App struct {
	Executable      string
	Params          map[string]string
	Binopts         map[string]string
	Defaults        map[string]string
	SGEJobResources string
	Container       string
}

type Job struct {
	Command          string
	Args             []string
	Container        string
	WorkingDirectory string
}

type Experiment struct {
	Id     int
	App    string
	Host   string
	User   int
	Params map[string]string
}

type ExperimentJobs struct {
	Map map[string]Experiment // maps job id to experiment
}

func run_job(job Job, sm drmaa2interface.SessionManager, js drmaa2interface.JobSession) drmaa2interface.Job {

	jt := drmaa2interface.JobTemplate{
		RemoteCommand:    job.Command,
		Args:             job.Args,
		JobCategory:      job.Container,
		WorkingDirectory: job.WorkingDirectory,
		OutputPath:       filepath.Join(job.WorkingDirectory, "output.txt"),
		JoinFiles:        true,
		// NativeSpecification: job.SGEJobResources,
	}
	// working dir is experiment dir, 2 levels up includes inputs and outputs
	root := filepath.Dir(filepath.Dir(job.WorkingDirectory))

	jt.StageInFiles = map[string]string{
		root: root,
	}

	os.Mkdir(job.WorkingDirectory, 0755)
	jr, err := js.RunJob(jt)
	if err != nil {
		panic(err)
	}

	return jr

}

func fetch_queue(apiUrl string, sm drmaa2interface.SessionManager, js drmaa2interface.JobSession) []Experiment {
	// fetch experiments from queue
	var queue []Experiment
	resp, err := http.Get(apiUrl + "/queue")
	if err != nil {
		log.Fatalln(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalln(err)
	}
	json.Unmarshal([]byte(body), &queue)

	return queue
}

func fetch_experiment(apiUrl string, experiment Experiment) Job {
	// fetch files for experiment
	eid := strconv.Itoa(experiment.Id)
	resp, err := http.Get(apiUrl + "/" + eid + "/files")
	if err != nil {
		log.Fatalln(err)
	}
	defer resp.Body.Close()
	zipfile := eid + ".zip"
	out, err := os.Create(zipfile)
	if err != nil {
		log.Fatalln(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		log.Fatalln(err)
	}
	// unzip fetched files
	input_dir := filepath.Join(INPUT_DIR, eid)
	if err := archiver.Unarchive(zipfile, input_dir); err != nil {
		log.Fatalln(err)
	}
	// delete zip
	os.Remove(zipfile)

	data, err := ioutil.ReadFile("../IPP-Experiment_Defintions/" + experiment.App + ".json")
	if err != nil {
		log.Fatalln(err)
	}
	var app App
	json.Unmarshal([]byte(data), &app)

	unused_defaults := app.Defaults
	experdir, _ := filepath.Abs(filepath.Join(OUTPUT_DIR, eid))
	// args := []string{app.Executable}
	args := []string{}
	for k, v := range experiment.Params {
		delete(unused_defaults, k) // just try deleting since removing a non-existent entry is a no-op
		if v == "" {
			v = app.Defaults[k]
		}
		if strings.HasPrefix(v, "$experdir") {
			v = filepath.Join(experdir, v[len("$experdir"):])
		} else if strings.HasPrefix(v, INPUT_DIR) {
			v, _ = filepath.Abs(
				filepath.Join(INPUT_DIR, eid, v[len(INPUT_DIR):]))
		}

		if _, ok := app.Params[k]; ok {
			args = append(args, app.Params[k]+" "+v)
		} else { // it must be in binopts
			args = append(args, app.Binopts[k])
		}
	}
	for k, v := range unused_defaults {
		if strings.HasPrefix(v, "$experdir") {
			v = filepath.Join(experdir, v[len("$experdir"):])
		}
		args = append(args, app.Params[k]+" "+v)
	}
	// fmt.Println(strings.Join(args, " "))
	executable := " "
	if app.Container == "" {
		executable = app.Executable
	}
	ret := Job{
		Command:          executable,
		Args:             args,
		Container:        app.Container,
		WorkingDirectory: experdir,
	}
	return ret

}

func push_results(eid int, uid int, apiUrl string) {
	// zip results dir
	experdir, _ := filepath.Abs(filepath.Join(OUTPUT_DIR, strconv.Itoa(eid)))
	zipfile := strconv.Itoa(eid) + ".zip"
	// todo could stream files into an archive that is being written to HTTP response w/o writing disk
	// see: https://github.com/mholt/archiver#library-use
	if err := archiver.Archive([]string{experdir}, zipfile); err != nil {
		log.Fatalln(err)
	}
	// upload zip
	client := resty.New()
	resp, err := client.R().
		SetFile("results", zipfile).
		Post(apiUrl + "/" + strconv.Itoa(eid) + "/results")
	fmt.Println(resp)
	if err != nil {
		log.Fatalln(err)
	}

	os.Remove(zipfile)
	os.RemoveAll(experdir)
}

func delete_inputs(eid int, apiUrl string) {
	client := resty.New()
	_, err := client.R().
		Delete(apiUrl + "/" + strconv.Itoa(eid) + "/delete")
	if err != nil {
		log.Fatalln(err)
	}
}

func mark_failed(eid int, exit_code drmaa2interface.JobState, apiUrl string) {
	client := resty.New()
	_, err := client.R().
		SetFormData(map[string]string{
			"exit_code": exit_code.String(),
		}).
		Post(apiUrl + "/" + strconv.Itoa(eid) + "/failed")
	if err != nil {
		log.Fatalln(err)
	}
}

func setup_tunnel() (*sshtunnel.SSHTunnel, string) {
	// Setup the tunnel, but do not yet start it yet.
	tunnel := sshtunnel.NewSSHTunnel(
		// User and host of tunnel server, it will default to port 22
		// if not specified.
		SSH_ADDR,

		// Pick ONE of the following authentication methods:
		// sshtunnel.PrivateKeyFile("path/to/private/key.pem"), // 1. private key
		ssh.Password("root"), // 2. password
		// sshtunnel.SSHAgent(),                                // 3. ssh-agent

		// The destination host and port of the actual server.
		REMOTE_ADDR,

		// The local port you want to bind the remote port to.
		// Specifying "0" will lead to a random port.
		LOCAL_PORT,
	)

	// You can provide a logger for debugging, or remove this line to
	// make it silent.
	tunnel.Log = log.New(os.Stdout, "", log.Ldate|log.Lmicroseconds)

	// Start the server in the background. You will need to wait a
	// small amount of time for it to bind to the localhost port
	// before you can start sending connections.
	go tunnel.Start()
	time.Sleep(100 * time.Millisecond)

	return tunnel, "http://localhost:" + strconv.Itoa(tunnel.Local.Port) + "/experiments"
}

func main() {
	backend := flag.String("backend", "libdrmaa", "backend type") // choices: libdrmaa, docker
	contactString := flag.String("contact", "", "contact string") // see https://github.com/dgruber/drmaa2os/issues/24
	submit := flag.Bool("submit", false, "submit jobs")
	status := flag.Bool("status", false, "status jobs")
	flag.Parse()

	// setup drmaa session
	var sm drmaa2interface.SessionManager
	switch *backend {
	case "docker":
		sm, _ = drmaa2os.NewDockerSessionManager("docker.db")
	case "libdrmaa":
		params := libdrmaa.LibDRMAASessionParams{
			ContactString:           "",
			UsePersistentJobStorage: true,
			DBFilePath:              "libdrmaajobs.db",
		}
		sm, _ = drmaa2os.NewLibDRMAASessionManagerWithParams(params, "libdrmaa.db")
	default:
		log.Fatalln("invalid backend")
	}

	js, err := sm.CreateJobSession("jobsession", *contactString)
	if err != nil {
		js, err = sm.OpenJobSession("jobsession")
		if err != nil {
			panic(err)
		}
	} else {
		contact, err := js.GetContact()
		if err != nil {
			panic(err)
		}
		fmt.Printf("session has contact string %s\n", contact)
	}
	defer js.Close()

	// setup ssh tunnel
	tunnel, apiUrl := setup_tunnel()

	// check if we previously submitted jobs
	var submitted ExperimentJobs
	dat, err := os.ReadFile("jobs.json")
	if err == nil {
		json.Unmarshal(dat, &submitted)
	} else {
		submitted.Map = make(map[string]Experiment)
	}
	if *status {
		// check if there are any jobs currently running
		filter := drmaa2interface.CreateJobInfo()
		existingJobs, err := js.GetJobs(filter)
		if err != nil {
			fmt.Printf("could not list jobs: %v\n", err)
		}
		for job, exp := range submitted.Map {
			found := false
			for _, existingJob := range existingJobs {
				if job == existingJob.GetID() {
					found = true
					break
				}
			}
			if !found {
				fmt.Println("Job completed")
				fmt.Println(job)
				delete(submitted.Map, job)
				delete_inputs(exp.Id, apiUrl)
				push_results(exp.Id, exp.User, apiUrl)
			}
		}
	}
	// switch state {
	// case drmaa2interface.Failed:
	// 	fmt.Println("Failed to execute job successfully")
	// 	tunnel.Start()
	// 	fmt.Println("Re opening tunnel")
	// 	mark_failed(experiment.Id, state, apiUrl)
	// 	tunnel.Close()
	// 	fmt.Println("Closing tunnel")
	// case drmaa2interface.Done:
	// 	fmt.Println("Completed successfully")
	// 	tunnel.Start()
	// 	fmt.Println("Re opening tunnel")
	// 	delete_inputs(experiment.Id, apiUrl)
	// 	push_results(experiment.Id, experiment.User, apiUrl)
	// 	tunnel.Close()
	// 	fmt.Println("Closing tunnel")
	// }

	if *submit {
		// fetch new jobs
		queue := fetch_queue(apiUrl, sm, js)
		for _, experiment := range queue {

			// fetch files for each experiment
			job := fetch_experiment(apiUrl, experiment)

			fmt.Println(job)
			submitted_job := run_job(job, sm, js)
			// write to json
			submitted.Map[submitted_job.GetID()] = experiment
			// write to file
			json_data, _ := json.Marshal(submitted)
			ioutil.WriteFile("jobs.json", json_data, 0644)

		}
	}

	tunnel.Close()
	js.Close()
	sm.DestroyJobSession("jobsession")

}
