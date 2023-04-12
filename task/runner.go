package task

import (
	"encoding/json"

	types "deploybot-service-launcher/deploybot-types"
	"deploybot-service-launcher/util"
)

type Runner struct {
	cHelper *util.ContainerHelper
}

func NewRunner() *Runner {

	return &Runner{util.NewContainerHelper()}
}

func (r *Runner) DoTask(t types.Task, arguments []string) error {

	var c types.DeployConfig

	bs, err := json.Marshal(t.Config)

	if err != nil {
		return err
	}

	err = json.Unmarshal(bs, &c)

	if err != nil {
		return err
	}

	go func() {
		r.cHelper.StartContainer(&c)
	}()

	return nil
}
