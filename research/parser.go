package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

// Results > Alternatives > Words

type Response struct {
	Results []Result `json:"results"`
}

type Result struct {
	Alternatives []Alternative `json:"alternatives"`
}

type Alternative struct {
	Transcript string  `json:"transcript"`
	Confidence float64 `json:"confidence"`
	Words      []Word  `json:"words"`
}

type Word struct {
	StartTime  string  `json:"startTime"`
	EndTime    string  `json:"endTime"`
	Word       string  `json:"word"`
	Confidence float64 `json:"confidence"`
	SpeakerTag int     `json:"speakerTag"`
}

func main() {

	// open
	jsonFile, err := os.Open("example.json")
	if err != nil {
		fmt.Println(err)
	}

	// defer the closing
	defer jsonFile.Close()

	// read in our opened json file
	byteValue, _ := ioutil.ReadAll(jsonFile)

	// init struct
	var response Response

	// parse json into struct
	json.Unmarshal(byteValue, &response)

	// loop through json and pull out the transcripts
	for _, result := range response.Results {
		for _, alternative := range result.Alternatives {
			fmt.Println(alternative.Transcript)
		}
	}

}
