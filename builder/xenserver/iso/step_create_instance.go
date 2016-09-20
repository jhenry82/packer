package iso

import (
	"fmt"
	"strconv"

	"github.com/mitchellh/multistep"
	"github.com/mitchellh/packer/packer"
	xsclient "github.com/xenserver/go-xenserver-client"
)

type stepCreateInstance struct {
	instance *xsclient.VM
	vdi      []*xsclient.VDI
}

func (self *stepCreateInstance) Run(state multistep.StateBag) multistep.StepAction {

	client := state.Get("client").(xsclient.XenAPIClient)
	config := state.Get("config").(config)
	ui := state.Get("ui").(packer.Ui)

	ui.Say("Step: Create Instance")

	// Get the template to clone from

	vms, err := client.GetVMByNameLabel(config.CloneTemplate)

	switch {
	case len(vms) == 0:
		ui.Error(fmt.Sprintf("Couldn't find a template with the name-label '%s'. Aborting.", config.CloneTemplate))
		return multistep.ActionHalt
	case len(vms) > 1:
		ui.Error(fmt.Sprintf("Found more than one template with the name '%s'. The name must be unique. Aborting.", config.CloneTemplate))
		return multistep.ActionHalt
	}

	template := vms[0]

	// Clone that VM template
	instance, err := template.Clone(config.VMName)
	if err != nil {
		ui.Error(fmt.Sprintf("Error cloning VM: %s", err.Error()))
		return multistep.ActionHalt
	}
	self.instance = instance

	err = instance.SetIsATemplate(false)
	if err != nil {
		ui.Error(fmt.Sprintf("Error setting is_a_template=false: %s", err.Error()))
		return multistep.ActionHalt
	}

	err = instance.SetStaticMemoryRange(uint64(config.VMMemory*1024*1024), uint64(config.VMMemory*1024*1024))
	if err != nil {
		ui.Error(fmt.Sprintf("Error setting VM memory=%d: %s", config.VMMemory*1024*1024, err.Error()))
		return multistep.ActionHalt
	}

	err = instance.SetPlatform(config.PlatformArgs)
	if err != nil {
		ui.Error(fmt.Sprintf("Error setting VM platform: %s", err.Error()))
		return multistep.ActionHalt
	}

	err = instance.SetVCpuMax(config.VMVCpus)
	if err != nil {
		ui.Error(fmt.Sprintf("Error setting maximum vcpus: %s", err.Error()))
		return multistep.ActionHalt
	}

	err = instance.SetVCpuAtStartup(config.VMVCpus)
	if err != nil {
		ui.Error(fmt.Sprintf("Error setting startup vcpus: %s", err.Error()))
		return multistep.ActionHalt
	}

	err = instance.SetDescription(config.VMDescription)
	if err != nil {
		ui.Error(fmt.Sprintf("Error setting VM description: %s", err.Error()))
		return multistep.ActionHalt
	}

	if len(config.VMOtherConfig) != 0 {
		vm_other_config, err := instance.GetOtherConfig()
		if err != nil {
			ui.Error(fmt.Sprintf("Error getting VM other-config: %s", err.Error()))
			return multistep.ActionHalt
		}
		for key, value := range config.VMOtherConfig {
			vm_other_config[key] = value
		}
		err = instance.SetOtherConfig(vm_other_config)
		if err != nil {
			ui.Error(fmt.Sprintf("Error setting VM other-config: %s", err.Error()))
			return multistep.ActionHalt
		}
	}

	// Create VDI for the instance

	sr, err := config.GetSrByName(client, config.SrName)
	if err != nil {
		ui.Error(fmt.Sprintf("Unable to get SR: %s", err.Error()))
		return multistep.ActionHalt
	}

	// Iterate over the nested slices of disk names and sizes provided, creating each pair
	for _, values := range config.VMDisks {
		diskname := values[0]
		disksize, _ := strconv.Atoi(values[1])
		ui.Say(fmt.Sprintf("Creating disk %s: %dMB", diskname, disksize))
		vdi, err := sr.CreateVdi(diskname, int64(disksize*1024*1024))
		if err != nil {
			ui.Error(fmt.Sprintf("Unable to create packer disk VDI: %s", err.Error()))
			return multistep.ActionHalt
		}
		self.vdi = append(self.vdi, vdi)

		err = instance.ConnectVdi(vdi, xsclient.Disk, "")
		if err != nil {
			ui.Error(fmt.Sprintf("Unable to connect packer disk VDI: %s", err.Error()))
			return multistep.ActionHalt
		}
	}
	// Connect Network

	var network *xsclient.Network

	if config.NetworkName == "" {
		// No network has be specified. Use the management interface
		network = new(xsclient.Network)
		network.Ref = ""
		network.Client = &client

		pifs, err := client.GetPIFs()

		if err != nil {
			ui.Error(fmt.Sprintf("Error getting PIFs: %s", err.Error()))
			return multistep.ActionHalt
		}

		for _, pif := range pifs {
			pif_rec, err := pif.GetRecord()

			if err != nil {
				ui.Error(fmt.Sprintf("Error getting PIF record: %s", err.Error()))
				return multistep.ActionHalt
			}

			if pif_rec["management"].(bool) {
				network.Ref = pif_rec["network"].(string)
			}

		}

		if network.Ref == "" {
			ui.Error("Error: couldn't find management network. Aborting.")
			return multistep.ActionHalt
		}

	} else {
		// Look up the network by it's name label

		networks, err := client.GetNetworkByNameLabel(config.NetworkName)

		if err != nil {
			ui.Error(fmt.Sprintf("Error occured getting Network by name-label: %s", err.Error()))
			return multistep.ActionHalt
		}

		switch {
		case len(networks) == 0:
			ui.Error(fmt.Sprintf("Couldn't find a network with the specified name-label '%s'. Aborting.", config.NetworkName))
			return multistep.ActionHalt
		case len(networks) > 1:
			ui.Error(fmt.Sprintf("Found more than one network with the name '%s'. The name must be unique. Aborting.", config.NetworkName))
			return multistep.ActionHalt
		}

		network = networks[0]
	}

	if err != nil {
		ui.Say(err.Error())
	}
	_, err = instance.ConnectNetwork(network, "0")

	if err != nil {
		ui.Say(err.Error())
	}

	instanceId, err := instance.GetUuid()
	if err != nil {
		ui.Error(fmt.Sprintf("Unable to get VM UUID: %s", err.Error()))
		return multistep.ActionHalt
	}

	state.Put("instance_uuid", instanceId)
	ui.Say(fmt.Sprintf("Created instance '%s'", instanceId))

	bootPolicy, err := instance.GetHVMBootPolicy()
	if err != nil {
		ui.Error(fmt.Sprintf("Unable to determine if VM is HVM or PV: %s", err.Error()))
		return multistep.ActionHalt
	}

	// XXX TODO HACK FIXME
	// Without this, the final VM cannot boot
	bootPolicy = ""
	state.Put("virtualization_type", bootPolicy)

	for index, vdis := range self.vdi {
		vdiId, err := vdis.GetUuid()
		if err != nil {
			ui.Error(fmt.Sprintf("Unable to get VM VDI UUID: %s", err.Error()))
			return multistep.ActionHalt
		}

		state.Put(fmt.Sprintf("instance_vdi_uuid_%d", index), vdiId)
		ui.Say(fmt.Sprintf("Attached vdi '%s'", vdiId))
	}

	srId, err := sr.GetUuid()
	if err != nil {
		ui.Error(fmt.Sprintf("Unable to get VDI SR UUID: %s", err.Error()))
		return multistep.ActionHalt
	}

	state.Put("instance_sr_uuid", srId)
	ui.Say(fmt.Sprintf("Using SR '%s'", srId))

	return multistep.ActionContinue
}

func (self *stepCreateInstance) Cleanup(state multistep.StateBag) {
	config := state.Get("config").(config)
	if config.ShouldKeepVM(state) {
		return
	}

	ui := state.Get("ui").(packer.Ui)

	if self.instance != nil {
		ui.Say("Destroying VM")
		_ = self.instance.HardShutdown() // redundant, just in case
		err := self.instance.Destroy()
		if err != nil {
			ui.Error(err.Error())
		}
	}

	// Destroy any VDI's we have created
	if self.vdi != nil {
		ui.Say("Destroying VDI's")
		for _, vdis := range self.vdi {
			err := vdis.Destroy()
			if err != nil {
				ui.Error(err.Error())
			}
		}
	}
}
