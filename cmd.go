package gosu

import (
	"bytes"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/mgutz/ansi"
	"github.com/mgutz/gosu/util"
	"github.com/mgutz/str"
)

// In is used by Bash(), Run() and Start() to set the working directory
type In []string

var spawnedProcesses = make(map[string]*os.Process)

// Bash executes a bash script (string) with an option to set
// the working directory.
func Bash(script string, wd ...In) error {
	_, err := bash(false, script, wd)
	return err
}

// BashOutput is the same as Bash and it captures stdout and stderr.
func BashOutput(script string, wd ...In) (string, error) {
	return bash(true, script, wd)
}

// Run runs a command with an an option to set the working directory.
func Run(commandstr string, wd ...In) error {
	_, err := run(false, commandstr, wd)
	return err
}

// RunOutput is same as Run and it captures stdout and stderr.
func RunOutput(commandstr string, wd ...In) (string, error) {
	return run(true, commandstr, wd)
}

// Start starts an async command. If executable has suffix ".go" then it will
// be "go install"ed then executed. Use this for watching a server task.
//
// If Start is called with the same command it kills the previous process.
//
// The working directory is optional.
func Start(commandstr string, wd ...In) error {
	dir, err := getWd(wd)
	if err != nil {
		return err
	}

	executable, argv, env := splitCommand(commandstr)
	isGoFile := strings.HasSuffix(executable, ".go")
	if isGoFile {
		err = Run("go install", wd...)
		if err != nil {
			return err
		}
		executable = path.Base(dir)
	}

	cmd := &command{
		executable: executable,
		wd:         dir,
		env:        env,
		argv:       argv,
	}
	return cmd.runAsync()
}

// Bash executes a bash string. Use backticks for multiline. To execute as shell script,
// use Run("bash script.sh")
func bash(captureOutput bool, script string, wd []In) (output string, err error) {
	dir, err := getWd(wd)
	if err != nil {
		return
	}

	gcmd := &command{
		executable:    "bash",
		argv:          []string{"-c", script},
		wd:            dir,
		captureOutput: captureOutput,
	}

	return gcmd.run()
}

func getWd(wd []In) (string, error) {
	if len(wd) == 1 {
		return wd[0][0], nil
	}
	return os.Getwd()
}

func run(captureOutput bool, commandstr string, wd []In) (output string, err error) {
	dir, err := getWd(wd)
	if err != nil {
		return
	}
	executable, argv, env := splitCommand(commandstr)

	cmd := &command{
		executable:    executable,
		wd:            dir,
		env:           env,
		argv:          argv,
		captureOutput: captureOutput,
	}
	return cmd.run()
}

func splitCommand(command string) (executable string, argv, env []string) {
	argv = str.ToArgv(command)
	for i, item := range argv {
		if strings.Contains(item, "=") {
			if env == nil {
				env = []string{item}
				continue
			}
			env = append(env, item)
		} else {
			executable = item
			argv = argv[i+1:]
			return
		}
	}

	executable = argv[0]
	argv = argv[1:]
	return
}

type command struct {
	executable    string
	argv          []string
	env           []string
	wd            string
	captureOutput bool
	recorder      bytes.Buffer
}

func (gcmd *command) hash() string {
	if len(gcmd.argv) > 0 {
		return gcmd.executable + strings.Join(gcmd.argv, ",")
	}
	return gcmd.executable
}

func (gcmd *command) toCmd() (cmd *exec.Cmd, err error) {
	cmd = exec.Command(gcmd.executable, gcmd.argv...)
	if gcmd.wd != "" {
		cmd.Dir = gcmd.wd
	}

	cmd.Env = effectiveEnv(gcmd.env)
	cmd.Stdin = os.Stdin

	if gcmd.captureOutput {
		outWrapper := newFileWrapper(os.Stdout, &gcmd.recorder, "")
		errWrapper := newFileWrapper(os.Stderr, &gcmd.recorder, ansi.ColorCode("red+b"))
		cmd.Stdout = outWrapper
		cmd.Stderr = errWrapper
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd, nil
}

func (gcmd *command) run() (output string, err error) {

	cmd, err := gcmd.toCmd()
	if err != nil {
		return
	}

	err = cmd.Run()
	if gcmd.captureOutput {
		return gcmd.recorder.String(), err
	}
	return "", err
}

func (gcmd *command) runAsync() (err error) {
	cmd, err := gcmd.toCmd()
	if err != nil {
		return
	}

	id := gcmd.hash()

	// kills previously spawned process (if exists)
	killSpawned(id)
	waitExit = true
	waitgroup.Add(1)
	go func() {
		err = cmd.Start()
		if err != nil {
			return
		}
		spawnedProcesses[id] = cmd.Process
		c := make(chan error, 1)
		c <- cmd.Wait()
		_ = <-c
		waitgroup.Done()
	}()
	return nil
}

func toInt(s string) int {
	result, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return result
}

func killSpawned(command string) {
	process := spawnedProcesses[command]
	if process == nil {
		return
	}

	err := process.Kill()
	if err != nil {
		util.Error("Start", "Could not kill existing process %+v\n", process)
		return
	}
}

// Inside temporarily changes the working directory and restores it when lambda
// finishes.
func Inside(dir string, lambda func()) error {
	olddir, err := os.Getwd()
	if err != nil {
		return err
	}

	err = os.Chdir(dir)
	if err != nil {
		return err
	}

	defer func() {
		os.Chdir(olddir)
	}()
	lambda()
	return nil
}
