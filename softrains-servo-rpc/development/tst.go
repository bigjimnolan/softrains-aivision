package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

func main() {
	// Check if the user passed both a chip line index and a microsecond value
	if len(os.Args) < 3 {
		fmt.Printf("Usage: sudo ./servo_cli <Line_Num> <Microseconds>\n")
		fmt.Printf("Example: sudo ./servo_cli 267 1500\n")
		os.Exit(1)
	}

	lineNum, err1 := strconv.Atoi(os.Args[1])
	pulse, err2 := strconv.Atoi(os.Args[2])
	if err1 != nil || err2 != nil {
		fmt.Println("Error: Line number and pulse must be valid integers.")
		os.Exit(1)
	}

	if pulse < 500 || pulse > 2500 {
		fmt.Println("Error: Pulse must be between 500 and 2500 microseconds.")
		os.Exit(1)
	}

	// Export the pin using the modern kernel path structure
	exportFile, err := os.OpenFile("/sys/class/gpio/export", os.O_WRONLY, 0644)
	if err == nil {
		exportFile.WriteString(strconv.Itoa(lineNum))
		exportFile.Close()
	}

	// Set pin direction to out
	dirPath := fmt.Sprintf("/sys/class/gpio/gpio%d/direction", lineNum)
	dirFile, err := os.OpenFile(dirPath, os.O_WRONLY, 0644)
	if err == nil {
		dirFile.WriteString("out")
		dirFile.Close()
	}

	// Open the direct hardware line value stream
	valPath := fmt.Sprintf("/sys/class/gpio/gpio%d/value", lineNum)
	valFile, err := os.OpenFile(valPath, os.O_WRONLY, 0644)
	if err != nil {
		fmt.Errorf("Error: Could not access hardware line %d. Check your live address mapping.\n", lineNum)
		os.Exit(1)
	}
	defer valFile.Close()

	totalPeriod := 20000 // 20ms in microseconds
	remainder := totalPeriod - pulse

	// Convert microseconds explicitly to Go's time.Duration (nanosecond base)
	pulseDuration := time.Duration(pulse) * time.Microsecond
	remainderDuration := time.Duration(remainder) * time.Microsecond

	fmt.Printf("Driving kernel line %d at %dus pulse width for 1.5 seconds...\n", lineNum, pulse)

	// Fire the hardware loops cleanly with absolute zero third-party dependencies
	// Go []byte arrays map directly to underlying C char array writes
	highBytes := []byte("1")
	lowBytes := []byte("0")

	for i := 0; i < 75; i++ {
		valFile.Write(highBytes)
		time.Sleep(pulseDuration)

		valFile.Write(lowBytes)
		time.Sleep(remainderDuration)
	}
}

