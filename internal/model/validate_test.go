package model

import "testing"

func TestProtocolSettingsAndFamilyAdmission(t *testing.T) {
	icmp := Target{Host: "::1", Measurement: ProbeSettings{Method: MethodICMP, Families: []string{FamilyIPv6}, PacketsPerCycle: 3, PacketSpacingMS: 1000, TimeoutMS: 1000, IntervalSeconds: 60}}
	if err := ValidateProbeTarget(icmp); err != nil {
		t.Fatal(err)
	}
	icmp.Measurement.Families = []string{FamilyIPv4}
	if ValidateProbeTarget(icmp) == nil {
		t.Fatal("wrong literal family accepted")
	}
	icmp.Admission = map[string]Admission{FamilyIPv4: {State: "unsupported", Reason: "literal IPv6 endpoint"}}
	if err := ValidateProbeTarget(icmp); err != nil {
		t.Fatal("explicitly unsupported definition should remain visible", err)
	}
	udp := Target{Host: "example.com", Port: 7770, Measurement: ProbeSettings{Method: MethodUDP, Families: []string{FamilyIPv4, FamilyIPv6}, PacketsPerCycle: 3, PacketSpacingMS: 1000, TimeoutMS: 2000, PayloadBytes: 32}}
	if err := ValidateProbeTarget(udp); err != nil {
		t.Fatal(err)
	}
	udp.Measurement.PacketsPerCycle = 4
	if ValidateProbeTarget(udp) == nil {
		t.Fatal("unbounded UDP burst accepted")
	}
	https := Target{Host: "example.com", Port: 443, Scheme: "https", Path: "/", Measurement: ProbeSettings{Method: MethodHTTPS, Families: []string{FamilyIPv4}, HTTPMethod: "HEAD", ExpectedStatusCodes: []int{200}}}
	// Recovered source HTTPS definitions use five-second application timeouts.
	// They must fit the shared desktop validator as well as the catalogue parser.
	https.Measurement.TimeoutMS = 5000
	if err := ValidateProbeTarget(https); err != nil {
		t.Fatal(err)
	}
	https.Measurement.TimeoutMS = 10001
	if ValidateProbeTarget(https) == nil {
		t.Fatal("unbounded HTTPS timeout accepted")
	}
	https.Measurement.TimeoutMS = 5000
	https.Measurement.HTTPMethod = "POST"
	if ValidateProbeTarget(https) == nil {
		t.Fatal("mutating HTTP request accepted")
	}
}
