"# GPIO Permission Fixes for Orange Pi

## Error Resolution

The error you're encountering:
```
2026/07/11 03:55:08 failed to initialize controller: set home position for pan: failed to set pin 13 as output: sysfs-gpio (GPIO13): write /sys/class/gpio/export: errno 517
```

This is a permissions issue. The `errno 517` typically means an operation is not supported or permission denied when trying to access GPIO pins.

## Solutions

### 1. Add User to GPIO Group (Recommended)
```bash
sudo usermod -a -G gpio bigjim
```
Then **reboot** your system for changes to take effect.

### 2. Run with Sudo (Temporary Fix)
```bash
sudo ./gimbalrpc-arm64
```

### 3. Check GPIO Access
After adding to group, verify access:
```bash
ls -la /dev/gpio*
```

## Important Notes

1. Once you add your user to the `gpio` group, **you must reboot** for permission changes to take effect
2. On some Orange Pi systems, you may also need:
   ```bash
   sudo modprobe gpio-sysfs
   ```

## Alternative Testing Approach

If GPIO permissions remain problematic, test with a dummy implementation:

```go
// In your orangepi.go, add this simple mock:
func (b *OrangePiBoard) SetDigital(pinNum int, on bool, activeHigh bool) error {
    // Mock implementation for testing without real hardware access
    fmt.Printf("Mock GPIO operation: pin %d set to %t\n", pinNum, on)
    return nil
}
```
"