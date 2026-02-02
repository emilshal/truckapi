package chrobinson

import (
	"os"
	"testing"
)

func TestParseAddress(t *testing.T) {
	os.Setenv("SOME_CONFIG", "value") // Set necessary environment variables if needed
	defer os.Unsetenv("SOME_CONFIG")  // Clean up after the test
	expected := Location{City: "New York", State: "NY", Zip: "10001", Country: "USA"}
	result := ParseAddress("349 W 30th St, New York, NY 10001, USA")
	if result != expected {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

func TestParseAddressDifferentFormat(t *testing.T) {
	os.Setenv("SOME_CONFIG", "value") // Set necessary environment variables if needed
	defer os.Unsetenv("SOME_CONFIG")  // Clean up after the test
	expected := Location{City: "Columbus", State: "OH", Zip: "43204", Country: "USA"}
	result := ParseAddress("Columbus, OH 43204, USA")
	if result != expected {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

func TestParseAddressIncomplete(t *testing.T) {
	os.Setenv("SOME_CONFIG", "value") // Set necessary environment variables if needed
	defer os.Unsetenv("SOME_CONFIG")  // Clean up after the test
	expected := Location{City: "", State: "", Zip: "", Country: ""}
	result := ParseAddress("New York")
	if result != expected {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}
