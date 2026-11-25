// Command mockbmc serves a small fake Redfish service for trying out Redfish Browser
// without real hardware. Credentials: admin / admin.
//
//	go run ./cmd/mockbmc -addr 127.0.0.1:8443
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

type obj = map[string]any

func ref(p string) obj { return obj{"@odata.id": p} }

func status(h string) obj { return obj{"Health": h, "State": "Enabled"} }

func coll(path, typ, name string, members ...string) obj {
	ms := []any{}
	for _, m := range members {
		ms = append(ms, ref(m))
	}
	return obj{"@odata.id": path, "@odata.type": "#" + typ + "." + typ, "Name": name,
		"Members": ms, "Members@odata.count": len(ms)}
}

func build() map[string]obj {
	r := map[string]obj{}
	add := func(o obj) { r[o["@odata.id"].(string)] = o }
	const v = "/redfish/v1"

	add(obj{"@odata.id": v, "@odata.type": "#ServiceRoot.v1_15_0.ServiceRoot", "Id": "RootService",
		"Name": "Root Service", "RedfishVersion": "1.17.0", "UUID": "3a2b1c00-1111-2222-3333-444455556666",
		"Vendor": "Mockup", "Product": "Mock BMC 3000",
		"Systems": ref(v + "/Systems"), "Chassis": ref(v + "/Chassis"), "Managers": ref(v + "/Managers"),
		"AccountService": ref(v + "/AccountService"), "SessionService": ref(v + "/SessionService"),
		"UpdateService": ref(v + "/UpdateService"), "EventService": ref(v + "/EventService"),
		"Links": obj{"Sessions": ref(v + "/SessionService/Sessions")}})

	add(coll(v+"/Systems", "ComputerSystemCollection", "Computer System Collection", v+"/Systems/1"))
	s := v + "/Systems/1"
	add(obj{"@odata.id": s, "@odata.type": "#ComputerSystem.v1_20_0.ComputerSystem", "Id": "1",
		"Name": "System", "SystemType": "Physical", "Manufacturer": "Contoso", "Model": "PowerServer R760",
		"SKU": "8XK2ZQ3", "SerialNumber": "CN7792161A0042", "PartNumber": "0X3D9F", "HostName": "web-node-07",
		"AssetTag": "IT-004213", "UUID": "4c4c4544-0058-4b10-8032-b8c04f5a5133", "BiosVersion": "2.3.5",
		"PowerState": "On", "IndicatorLED": "Off", "LastResetTime": "2026-11-20T08:14:03+00:00",
		"Status":           obj{"Health": "OK", "HealthRollup": "Warning", "State": "Enabled"},
		"ProcessorSummary": obj{"Count": 2, "CoreCount": 64, "Model": "Intel(R) Xeon(R) Platinum 8480+ CPU @ 2.00GHz", "Status": status("OK")},
		"MemorySummary":    obj{"TotalSystemMemoryGiB": 512, "MemoryMirroring": "System", "Status": status("OK")},
		"Boot": obj{"BootSourceOverrideEnabled": "Disabled", "BootSourceOverrideTarget": "None",
			"BootSourceOverrideMode": "UEFI", "BootSourceOverrideTarget@Redfish.AllowableValues": []any{"None", "Pxe", "Hdd", "Cd", "BiosSetup"}},
		"TrustedModules": []any{obj{"FirmwareVersion": "7.2.2.0", "InterfaceType": "TPM2_0", "Status": status("OK")}},
		"Processors":     ref(s + "/Processors"), "Memory": ref(s + "/Memory"), "Storage": ref(s + "/Storage"),
		"EthernetInterfaces": ref(s + "/EthernetInterfaces"), "Bios": ref(s + "/Bios"), "LogServices": ref(s + "/LogServices"),
		"Links": obj{"Chassis": []any{ref(v + "/Chassis/1")}, "ManagedBy": []any{ref(v + "/Managers/1")}},
		"Actions": obj{"#ComputerSystem.Reset": obj{"target": s + "/Actions/ComputerSystem.Reset",
			"ResetType@Redfish.AllowableValues": []any{"On", "ForceOff", "GracefulShutdown", "ForceRestart", "PowerCycle"}}},
		"Oem": obj{"Contoso": obj{"SystemGeneration": "16G Monolithic", "CPLDVersion": "1.0.6", "CoolingRollupStatus": "OK"}}})

	add(coll(s+"/Processors", "ProcessorCollection", "Processors", s+"/Processors/CPU1", s+"/Processors/CPU2"))
	for i := 1; i <= 2; i++ {
		add(obj{"@odata.id": fmt.Sprintf("%s/Processors/CPU%d", s, i), "@odata.type": "#Processor.v1_18_0.Processor",
			"Id": fmt.Sprintf("CPU%d", i), "Name": fmt.Sprintf("CPU %d", i), "Socket": fmt.Sprintf("CPU.Socket.%d", i),
			"Manufacturer": "Intel", "Model": "Intel(R) Xeon(R) Gold 6448Y", "ProcessorType": "CPU",
			"ProcessorArchitecture": "x86", "InstructionSet": "x86-64", "MaxSpeedMHz": 4100, "OperatingSpeedMHz": 2100,
			"TotalCores": 32, "TotalThreads": 64, "Status": status("OK"),
			"ProcessorId": obj{"VendorId": "GenuineIntel", "IdentificationRegisters": "0x000806F8", "MicrocodeInfo": "0x2B0004D0"}})
	}

	var dimms []string
	for i := 1; i <= 8; i++ {
		p := fmt.Sprintf("%s/Memory/DIMM.A%d", s, i)
		dimms = append(dimms, p)
		h := "OK"
		if i == 6 {
			h = "Warning"
		}
		add(obj{"@odata.id": p, "@odata.type": "#Memory.v1_17_0.Memory", "Id": fmt.Sprintf("DIMM.A%d", i),
			"Name": fmt.Sprintf("DIMM A%d", i), "CapacityMiB": 65536, "MemoryDeviceType": "DDR5",
			"OperatingSpeedMhz": 4800, "Manufacturer": "Hynix", "PartNumber": "HMCG94AEBRA109N",
			"SerialNumber": fmt.Sprintf("80AD0122%04X", i*7919), "RankCount": 2, "DataWidthBits": 64,
			"Status": status(h), "MemoryLocation": obj{"Socket": 1, "Channel": i, "Slot": 1}})
	}
	add(coll(s+"/Memory", "MemoryCollection", "Memory", dimms...))

	add(coll(s+"/Storage", "StorageCollection", "Storage", s+"/Storage/RAID.1"))
	st := s + "/Storage/RAID.1"
	var drives []any
	for i := 0; i < 4; i++ {
		p := fmt.Sprintf("%s/Drives/Disk.%d", st, i)
		drives = append(drives, ref(p))
		add(obj{"@odata.id": p, "@odata.type": "#Drive.v1_16_0.Drive", "Id": fmt.Sprintf("Disk.%d", i),
			"Name": fmt.Sprintf("Physical Disk %d", i), "MediaType": "SSD", "Protocol": "SAS",
			"CapacityBytes": 1920383410176, "Manufacturer": "SAMSUNG", "Model": "MZILT1T9HBJR",
			"SerialNumber": fmt.Sprintf("S5G0NE0R%03d", i), "Revision": "DSG8", "PredictedMediaLifeLeftPercent": 98 - i,
			"Status": status("OK")})
	}
	add(obj{"@odata.id": st, "@odata.type": "#Storage.v1_15_0.Storage", "Id": "RAID.1", "Name": "PERC H965i",
		"Status": status("OK"), "Drives": drives, "Drives@odata.count": len(drives),
		"Volumes": ref(st + "/Volumes"),
		"StorageControllers": []any{obj{"@odata.id": st + "#/StorageControllers/0", "MemberId": "0", "Name": "PERC H965i Front",
			"Manufacturer": "Broadcom", "FirmwareVersion": "8.4.0.0.18", "SpeedGbps": 24, "Status": status("OK"),
			"SupportedRAIDTypes": []any{"RAID0", "RAID1", "RAID5", "RAID6", "RAID10"}}}})
	add(coll(st+"/Volumes", "VolumeCollection", "Volumes", st+"/Volumes/Disk.Virtual.0"))
	add(obj{"@odata.id": st + "/Volumes/Disk.Virtual.0", "@odata.type": "#Volume.v1_9_0.Volume", "Id": "Disk.Virtual.0",
		"Name": "OS Volume", "RAIDType": "RAID10", "CapacityBytes": 3840766820352, "Status": status("OK"),
		"Links": obj{"Drives": drives}})

	add(coll(s+"/EthernetInterfaces", "EthernetInterfaceCollection", "Ethernet Interfaces", s+"/EthernetInterfaces/NIC.1", s+"/EthernetInterfaces/NIC.2"))
	for i := 1; i <= 2; i++ {
		link := "LinkUp"
		if i == 2 {
			link = "LinkDown"
		}
		add(obj{"@odata.id": fmt.Sprintf("%s/EthernetInterfaces/NIC.%d", s, i), "@odata.type": "#EthernetInterface.v1_9_0.EthernetInterface",
			"Id": fmt.Sprintf("NIC.%d", i), "Name": fmt.Sprintf("Embedded NIC %d", i), "MACAddress": fmt.Sprintf("B0:7B:25:DE:1A:%02X", i),
			"SpeedMbps": 25000, "LinkStatus": link, "Status": status("OK"), "AutoNeg": true, "FullDuplex": true,
			"IPv4Addresses": []any{obj{"Address": fmt.Sprintf("10.0.%d.17", i), "SubnetMask": "255.255.255.0", "Gateway": "10.0.1.1", "AddressOrigin": "DHCP"}}})
	}

	add(obj{"@odata.id": s + "/Bios", "@odata.type": "#Bios.v1_2_0.Bios", "Id": "Bios", "Name": "BIOS Configuration",
		"AttributeRegistry": "BiosAttributeRegistry.v1_0_0", "Attributes": obj{"BootMode": "Uefi", "LogicalProc": "Enabled",
			"ProcVirtualization": "Enabled", "SysProfile": "PerfPerWattOptimizedOs", "SecureBoot": "Enabled", "SriovGlobalEnable": "Disabled",
			"MemTest": "Disabled", "SerialComm": "OnConRedirCom2"}})

	add(coll(s+"/LogServices", "LogServiceCollection", "Log Services", s+"/LogServices/SEL"))
	add(obj{"@odata.id": s + "/LogServices/SEL", "@odata.type": "#LogService.v1_3_0.LogService", "Id": "SEL",
		"Name": "System Event Log", "MaxNumberOfRecords": 1024, "OverWritePolicy": "WrapsWhenFull", "ServiceEnabled": true,
		"Status": status("OK"), "Entries": ref(s + "/LogServices/SEL/Entries")})
	var entries []any
	msgs := []struct{ sev, msg string }{{"OK", "The system was powered on."}, {"Warning", "Correctable memory error rate exceeded for DIMM A6."},
		{"OK", "Log cleared."}, {"Critical", "Fan 3 RPM is less than the lower critical threshold."}, {"OK", "Fan 3 RPM is within range."}}
	for i, m := range msgs {
		entries = append(entries, obj{"@odata.id": fmt.Sprintf("%s/LogServices/SEL/Entries/%d", s, i+1), "Id": fmt.Sprint(i + 1),
			"Name": "Log Entry", "EntryType": "SEL", "Severity": m.sev, "Message": m.msg, "MessageId": fmt.Sprintf("SYS%04d", 100+i),
			"Created": fmt.Sprintf("2026-11-2%dT10:%02d:00+00:00", i, i*7)})
	}
	add(obj{"@odata.id": s + "/LogServices/SEL/Entries", "@odata.type": "#LogEntryCollection.LogEntryCollection",
		"Name": "Log Entries", "Members": entries, "Members@odata.count": len(entries)})

	add(coll(v+"/Chassis", "ChassisCollection", "Chassis Collection", v+"/Chassis/1"))
	c := v + "/Chassis/1"
	add(obj{"@odata.id": c, "@odata.type": "#Chassis.v1_23_0.Chassis", "Id": "1", "Name": "Main Chassis",
		"ChassisType": "RackMount", "Manufacturer": "Contoso", "Model": "PowerServer R760", "SerialNumber": "CN7792161A0042",
		"PowerState": "On", "Status": obj{"Health": "Warning", "HealthRollup": "Warning", "State": "Enabled"},
		"Location": obj{"PartLocation": obj{"LocationType": "Slot"}, "Placement": obj{"Rack": "R12", "Row": "B", "RackOffset": 22}},
		"Thermal":  ref(c + "/Thermal"), "Power": ref(c + "/Power"),
		"Links": obj{"ComputerSystems": []any{ref(s)}, "ManagedBy": []any{ref(v + "/Managers/1")}}})
	var temps, fans []any
	for i, t := range []struct {
		n   string
		r   float64
		ctx string
	}{{"System Board Inlet Temp", 23, "Intake"}, {"System Board Exhaust Temp", 38, "Exhaust"}, {"CPU1 Temp", 61, "CPU"}, {"CPU2 Temp", 58, "CPU"}, {"DIMM A6 Temp", 44, "Memory"}} {
		temps = append(temps, obj{"@odata.id": fmt.Sprintf("%s/Thermal#/Temperatures/%d", c, i), "MemberId": fmt.Sprint(i),
			"Name": t.n, "ReadingCelsius": t.r, "UpperThresholdNonCritical": 80, "UpperThresholdCritical": 90,
			"LowerThresholdCritical": 3, "PhysicalContext": t.ctx, "Status": status("OK"), "RelatedItem": []any{ref(s)}})
	}
	for i := 1; i <= 6; i++ {
		h, rpm := "OK", 7200+i*120
		if i == 3 {
			h, rpm = "Critical", 840
		}
		fans = append(fans, obj{"@odata.id": fmt.Sprintf("%s/Thermal#/Fans/%d", c, i-1), "MemberId": fmt.Sprint(i - 1),
			"Name": fmt.Sprintf("System Board Fan%d", i), "Reading": rpm, "ReadingUnits": "RPM", "LowerThresholdCritical": 960,
			"PhysicalContext": "SystemBoard", "Status": status(h)})
	}
	add(obj{"@odata.id": c + "/Thermal", "@odata.type": "#Thermal.v1_7_0.Thermal", "Id": "Thermal", "Name": "Thermal",
		"Temperatures": temps, "Fans": fans})
	add(obj{"@odata.id": c + "/Power", "@odata.type": "#Power.v1_7_1.Power", "Id": "Power", "Name": "Power",
		"PowerControl": []any{obj{"@odata.id": c + "/Power#/PowerControl/0", "MemberId": "0", "Name": "System Power Control",
			"PowerConsumedWatts": 412, "PowerCapacityWatts": 2400,
			"PowerMetrics": obj{"AverageConsumedWatts": 398, "MaxConsumedWatts": 655, "MinConsumedWatts": 201, "IntervalInMin": 60}}},
		"PowerSupplies": []any{
			obj{"@odata.id": c + "/Power#/PowerSupplies/0", "MemberId": "0", "Name": "PS1", "Manufacturer": "Delta", "Model": "D1400E-S2",
				"PowerCapacityWatts": 1400, "PowerInputWatts": 221, "PowerOutputWatts": 206, "FirmwareVersion": "00.1B.53", "Status": status("OK")},
			obj{"@odata.id": c + "/Power#/PowerSupplies/1", "MemberId": "1", "Name": "PS2", "Manufacturer": "Delta", "Model": "D1400E-S2",
				"PowerCapacityWatts": 1400, "PowerInputWatts": 213, "PowerOutputWatts": 198, "FirmwareVersion": "00.1B.53", "Status": status("OK")}},
		"Voltages": []any{obj{"MemberId": "0", "Name": "PS1 Voltage", "ReadingVolts": 230, "Status": status("OK")},
			obj{"MemberId": "1", "Name": "PS2 Voltage", "ReadingVolts": 229, "Status": status("OK")}}})

	add(coll(v+"/Managers", "ManagerCollection", "Manager Collection", v+"/Managers/1"))
	m := v + "/Managers/1"
	add(obj{"@odata.id": m, "@odata.type": "#Manager.v1_17_0.Manager", "Id": "1", "Name": "Manager",
		"ManagerType": "BMC", "Model": "MockBMC 9", "FirmwareVersion": "7.10.30.00", "UUID": "3a2b1c00-aaaa-bbbb-cccc-444455556666",
		"DateTime": "2026-11-25T10:00:00+00:00", "PowerState": "On", "Status": status("OK"),
		"EthernetInterfaces": ref(m + "/EthernetInterfaces"),
		"Links":              obj{"ManagerForServers": []any{ref(s)}, "ManagerForChassis": []any{ref(c)}},
		"Actions":            obj{"#Manager.Reset": obj{"target": m + "/Actions/Manager.Reset", "ResetType@Redfish.AllowableValues": []any{"GracefulRestart"}}}})
	add(coll(m+"/EthernetInterfaces", "EthernetInterfaceCollection", "Manager NICs", m+"/EthernetInterfaces/iDRAC.1"))
	add(obj{"@odata.id": m + "/EthernetInterfaces/iDRAC.1", "@odata.type": "#EthernetInterface.v1_9_0.EthernetInterface",
		"Id": "iDRAC.1", "Name": "BMC Network Interface", "MACAddress": "B0:7B:25:DE:FF:01", "HostName": "bmc-web-node-07",
		"SpeedMbps": 1000, "Status": status("OK"),
		"IPv4Addresses": []any{obj{"Address": "192.168.50.17", "SubnetMask": "255.255.255.0", "Gateway": "192.168.50.1", "AddressOrigin": "Static"}},
		"NameServers":   []any{"192.168.50.2", "1.1.1.1"}})

	add(obj{"@odata.id": v + "/AccountService", "@odata.type": "#AccountService.v1_12_0.AccountService", "Id": "AccountService",
		"Name": "Account Service", "ServiceEnabled": true, "MinPasswordLength": 8, "MaxPasswordLength": 40,
		"AccountLockoutThreshold": 5, "AccountLockoutDuration": 300, "Accounts": ref(v + "/AccountService/Accounts")})
	add(coll(v+"/AccountService/Accounts", "ManagerAccountCollection", "Accounts", v+"/AccountService/Accounts/2"))
	add(obj{"@odata.id": v + "/AccountService/Accounts/2", "@odata.type": "#ManagerAccount.v1_10_0.ManagerAccount",
		"Id": "2", "Name": "User Account", "UserName": "admin", "RoleId": "Administrator", "Enabled": true, "Locked": false})
	add(obj{"@odata.id": v + "/SessionService", "@odata.type": "#SessionService.v1_1_8.SessionService", "Id": "SessionService",
		"Name": "Session Service", "ServiceEnabled": true, "SessionTimeout": 1800, "Sessions": ref(v + "/SessionService/Sessions")})
	add(coll(v+"/SessionService/Sessions", "SessionCollection", "Sessions"))
	add(obj{"@odata.id": v + "/UpdateService", "@odata.type": "#UpdateService.v1_11_0.UpdateService", "Id": "UpdateService",
		"Name": "Update Service", "ServiceEnabled": true, "FirmwareInventory": ref(v + "/UpdateService/FirmwareInventory")})
	add(coll(v+"/UpdateService/FirmwareInventory", "SoftwareInventoryCollection", "Firmware Inventory",
		v+"/UpdateService/FirmwareInventory/BIOS", v+"/UpdateService/FirmwareInventory/BMC"))
	add(obj{"@odata.id": v + "/UpdateService/FirmwareInventory/BIOS", "@odata.type": "#SoftwareInventory.v1_8_0.SoftwareInventory",
		"Id": "BIOS", "Name": "BIOS", "Version": "2.3.5", "Updateable": true, "Status": status("OK")})
	add(obj{"@odata.id": v + "/UpdateService/FirmwareInventory/BMC", "@odata.type": "#SoftwareInventory.v1_8_0.SoftwareInventory",
		"Id": "BMC", "Name": "BMC Firmware", "Version": "7.10.30.00", "Updateable": true, "Status": status("OK")})
	add(obj{"@odata.id": v + "/EventService", "@odata.type": "#EventService.v1_9_0.EventService", "Id": "EventService",
		"Name": "Event Service", "ServiceEnabled": true, "DeliveryRetryAttempts": 3,
		"EventTypesForSubscription": []any{"Alert", "StatusChange"}, "Status": status("OK")})
	return r
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8443", "listen address")
	plain := flag.Bool("http", false, "serve plain HTTP instead of HTTPS")
	flag.Parse()

	res := build()
	var mu sync.Mutex
	tokens := map[string]bool{}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimRight(r.URL.Path, "/")
		log.Printf("%s %s", r.Method, p)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && p == "/redfish/v1/SessionService/Sessions" {
			var c struct{ UserName, Password string }
			json.NewDecoder(r.Body).Decode(&c)
			if c.UserName != "admin" || c.Password != "admin" {
				http.Error(w, `{"error":{"message":"Invalid credentials"}}`, http.StatusUnauthorized)
				return
			}
			b := make([]byte, 16)
			rand.Read(b)
			tok := hex.EncodeToString(b)
			mu.Lock()
			tokens[tok] = true
			mu.Unlock()
			w.Header().Set("X-Auth-Token", tok)
			w.Header().Set("Location", "/redfish/v1/SessionService/Sessions/"+tok[:8])
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if p != "/redfish/v1" {
			mu.Lock()
			ok := tokens[r.Header.Get("X-Auth-Token")]
			mu.Unlock()
			if u, pw, basic := r.BasicAuth(); basic && u == "admin" && pw == "admin" {
				ok = true
			}
			if !ok {
				http.Error(w, `{"error":{"message":"Unauthorized"}}`, http.StatusUnauthorized)
				return
			}
		}
		o, ok := res[p]
		if !ok {
			http.Error(w, `{"error":{"message":"Resource not found"}}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(o)
	})

	if *plain {
		log.Printf("mock Redfish service on http://%s (admin/admin)", *addr)
		log.Fatal(http.ListenAndServe(*addr, h))
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener.Close()
	l, err := netListen(*addr)
	if err != nil {
		log.Fatal(err)
	}
	srv.Listener = l
	srv.StartTLS()
	log.Printf("mock Redfish service on %s (self-signed TLS, admin/admin)", srv.URL)
	select {}
}

func netListen(addr string) (net.Listener, error) { return net.Listen("tcp", addr) }
