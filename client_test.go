package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestHumanize(t *testing.T) {
	for in, want := range map[string]string{
		"SerialNumber": "Serial Number", "IPv4Addresses": "IPv4 Addresses", "UUID": "UUID",
		"TotalSystemMemoryGiB": "Total System Memory GiB", "PCIeDevices": "PCIe Devices",
	} {
		if got := humanize(in); got != want {
			t.Errorf("humanize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientErrors(t *testing.T) {
	host := os.Getenv("REDFISH_MOCK")
	if host == "" {
		t.Skip("set REDFISH_MOCK")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _ := NewClient(host, "admin", "wrong", true)
	if err := c.Connect(ctx); err == nil {
		t.Fatal("expected auth failure")
	} else {
		t.Log("wrong password:", err)
	}
	c, _ = NewClient("127.0.0.1:1", "admin", "admin", true)
	if err := c.Connect(ctx); err == nil {
		t.Fatal("expected connection failure")
	} else {
		t.Log("unreachable:", err)
	}
	c, _ = NewClient("https://"+host+"/", "admin", "admin", true)
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := c.Get(ctx, "/redfish/v1/Chassis/1/Thermal#/Fans/2", false)
	if err != nil || r.Data["Name"] != "System Board Fan3" {
		t.Fatalf("fragment: %v %v", err, r)
	}
}
