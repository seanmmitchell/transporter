package jsto

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/seanmmitchell/ale/v2"
	"github.com/seanmmitchell/transporter"
)

type JSONConfig struct {
	FileLock *sync.Mutex
	FilePath string
}

func (conf *JSONConfig) Load(le *ale.LogEngine) (map[string]interface{}, error) {
	conf.FileLock.Lock()
	defer conf.FileLock.Unlock()
	le.Log(ale.Info, "Loading JSON File...")

	le.Log(ale.Verbose, "Opening JSON File...")
	file, err := os.Open(conf.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			le.Log(ale.Warning, "JSON File does not exist.")
			return nil, fmt.Errorf("json file does not exist")
		}
		le.Log(ale.Error, "\t==> Failed to open JSON file. Error: "+err.Error())
		return nil, fmt.Errorf("failed to open json file")
	}
	le.Log(ale.Verbose, "JSON File Opened.")

	le.Log(ale.Verbose, "Reading JSON File...")
	allBytes, err := io.ReadAll(file)
	if err != nil {
		le.Log(ale.Error, "\t==> Failed to read all of JSON. Error: "+err.Error())
		return nil, fmt.Errorf("failed to read json file")
	}
	le.Log(ale.Verbose, "JSON File Read.")

	le.Log(ale.Verbose, "Unmarshaling JSON File...")
	var jsonData map[string]interface{}
	err = json.Unmarshal(allBytes, &jsonData)
	if err != nil {
		le.Log(ale.Error, "\t==> Failed to unmarshal JSON. Error: "+err.Error())
		return nil, fmt.Errorf("failed to unmarshal json file")
	}
	le.Log(ale.Verbose, "JSON File Unmarshaled.")

	le.Log(ale.Info, "JSON File Loaded.")
	return jsonData, nil
}

func (conf *JSONConfig) Save(le *ale.LogEngine, pattern *transporter.Pattern) error {
	conf.FileLock.Lock()
	defer conf.FileLock.Unlock()
	le.Log(ale.Info, "Saving JSON File...")

	data, err := json.MarshalIndent(pattern.Sequences, "", "\t")
	if err != nil {
		le.Log(ale.Error, "Failed to marshal. Err: "+err.Error())
		return err
	}

	err = os.WriteFile(conf.FilePath, data, 0644)
	if err != nil {
		le.Log(ale.Error, "Failed to write. Err: "+err.Error())
		return err
	}

	le.Log(ale.Info, "JSON File Saved.")
	return nil
}
