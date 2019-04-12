package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"time"
)

//
// https://github.com/BenLubar/video/blob/master/ffprobe.go
//

// ProbeData -
type ProbeData struct {
	Status string       `json:"status"`
	Format *ProbeFormat `json:"format,omitempty"`
}

// ProbeFormat -
type ProbeFormat struct {
	Filename         string            `json:"filename"`
	NBStreams        int               `json:"nb_streams"`
	NBPrograms       int               `json:"nb_programs"`
	FormatName       string            `json:"format_name"`
	FormatLongName   string            `json:"format_long_name"`
	StartTimeSeconds float64           `json:"start_time,string"`
	DurationSeconds  float64           `json:"duration,string"`
	Size             uint64            `json:"size,string"`
	BitRate          uint64            `json:"bit_rate,string"`
	ProbeScore       float64           `json:"probe_score"`
	Tags             map[string]string `json:"tags"`
}

// StartTime -
func (f ProbeFormat) StartTime() time.Duration {
	return time.Duration(f.StartTimeSeconds * float64(time.Second))
}

// Duration -
func (f ProbeFormat) Duration() time.Duration {
	return time.Duration(f.DurationSeconds * float64(time.Second))
}

// Probe -
func Probe(filename string) (*ProbeData, error) {

	// https://gist.github.com/nrk/2286511 (json format struct from ffprobe)
	// configure the ffprobe command (using the current working directory)
	// ffprobe -v quiet -print_format json -show_format -show_streams "lolwut.mp4" > "lolwut.mp4.json"
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", filename)
	cmd.Stderr = os.Stderr

	r, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	// dump contents of command
	//fmt.Println(r)

	var v ProbeData
	err = json.NewDecoder(r).Decode(&v)
	if err != nil {
		return nil, err
	}

	err = cmd.Wait()
	if err != nil {
		return nil, err
	}

	return &v, nil
}
