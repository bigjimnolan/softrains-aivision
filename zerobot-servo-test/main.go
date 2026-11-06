package main

import (
    "fmt"
    "log"
    "time"

    "periph.io/x/conn/v3/gpio"
    "periph.io/x/conn/v3/physic"
    "periph.io/x/host/v3"
    "periph.io/x/host/v3/rpi"
)

func main() {
    // Initialize periph host drivers
    if _, err := host.Init(); err != nil {
        log.Fatal(err)
    }

    // Map to Physical Pin 12 (GPIO 18)
    pin := rpi.P1_12 

    // Set pin to PWM mode at standard 50Hz for hobby servos
    if err := pin.PWM(gpio.DutyMax/20, 50*physic.Hertz); err != nil {
        log.Fatalf("Failed to initialize PWM: %v", err)
    }

    fmt.Println("Running servo. Press Ctrl+C to exit.")
    for {
        fmt.Println("Moving to 0°")
        pin.PWM(gpio.DutyMax*1/20, 50*physic.Hertz) // ~1ms pulse width
        time.Sleep(2 * time.Second)

        fmt.Println("Moving to 90°")
        pin.PWM(gpio.DutyMax*3/40, 50*physic.Hertz) // ~1.5ms pulse width
        time.Sleep(2 * time.Second)

        fmt.Println("Moving to 180°")
        pin.PWM(gpio.DutyMax*2/20, 50*physic.Hertz) // ~2ms pulse width
        time.Sleep(2 * time.Second)
    }
}

