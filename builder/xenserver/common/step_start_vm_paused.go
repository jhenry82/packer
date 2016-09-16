package common

import (
	"fmt"
	"github.com/mitchellh/multistep"
	"github.com/mitchellh/packer/packer"
	xsclient "github.com/xenserver/go-xenserver-client"
	"log"
	"time"
)

type StepStartVmPaused struct{}

func (self *StepStartVmPaused) Run(state multistep.StateBag) multistep.StepAction {

	client := state.Get("client").(xsclient.XenAPIClient)
	ui := state.Get("ui").(packer.Ui)

	ui.Say("Step: Start VM Paused")

	uuid := state.Get("instance_uuid").(string)
	instance, err := client.GetVMByUuid(uuid)
	if err != nil {
		ui.Error(fmt.Sprintf("Unable to get VM from UUID '%s': %s", uuid, err.Error()))
		return multistep.ActionHalt
	}

	err = instance.SetHVMBoot("BIOS order", "cd")
	if err != nil {
		ui.Error(fmt.Sprintf("Unable to set HVM boot params: %s", err.Error()))
		return multistep.ActionHalt
	}

	err = instance.Start(true, false)
	if err != nil {
		ui.Error(fmt.Sprintf("Unable to start VM with UUID '%s': %s", uuid, err.Error()))
		time.Sleep(100000 * time.Millisecond)
		return multistep.ActionHalt
	}

	err = FindResidentHost(state, instance, uuid)
	if err != nil {
		ui.Error(fmt.Sprintf("Unable to find the host VM '%s' is on: %s", uuid, err.Error()))
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (self *StepStartVmPaused) Cleanup(state multistep.StateBag) {
	config := state.Get("commonconfig").(CommonConfig)
	client := state.Get("client").(xsclient.XenAPIClient)

	if config.ShouldKeepVM(state) {
		return
	}

	uuid := state.Get("instance_uuid").(string)
	instance, err := client.GetVMByUuid(uuid)
	if err != nil {
		log.Printf(fmt.Sprintf("Unable to get VM from UUID '%s': %s", uuid, err.Error()))
		return
	}

	err = instance.HardShutdown()
	if err != nil {
		log.Printf(fmt.Sprintf("Unable to force shutdown VM '%s': %s", uuid, err.Error()))
	}
}
