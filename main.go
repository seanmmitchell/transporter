package main

import (
	"github.com/seanmmitchell/ale"
	"github.com/seanmmitchell/ale/pconsole"
)

func main() {
	le := ale.CreateLogEngine("test")
	le.AddLogPipeline(ale.Info, pconsole.Log)
	le.Log(ale.Info, "Test")
}
