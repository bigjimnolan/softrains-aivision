#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>

int main(int argc, char *argv[]) {
    // Check if the user passed both a chip line index and a microsecond value
    if (argc < 3) {
        printf("Usage: sudo ./servo_cli <Line_Num> <Microseconds>\n");
        printf("Example: sudo ./servo_cli 267 1500\n");
        return 1;
    }

    int line_num = atoi(argv[1]);
    int pulse = atoi(argv[2]);
    
    if (pulse < 500 || pulse > 2500) {
        printf("Error: Pulse must be between 500 and 2500 microseconds.\n");
        return 1;
    }

    // Export the pin using the modern kernel path structure
    char path[64];
    int export_fd = open("/sys/class/gpio/export", O_WRONLY);
    if (export_fd >= 0) {
        dprintf(export_fd, "%d", line_num);
        close(export_fd);
    }

    // Set pin direction to out
    sprintf(path, "/sys/class/gpio/gpio%d/direction", line_num);
    int dir_fd = open(path, O_WRONLY);
    if (dir_fd >= 0) {
        dprintf(dir_fd, "out");
        close(dir_fd);
    }

    // Open the direct hardware line value stream
    sprintf(path, "/sys/class/gpio/gpio%d/value", line_num);
    int val_fd = open(path, O_WRONLY);
    if (val_fd < 0) {
        printf("Error: Could not access hardware line %d. Check your live address mapping.\n", line_num);
        return 1;
    }

    int total_period = 20000; // 20ms in microseconds
    int remainder = total_period - pulse;

    printf("Driving kernel line %d at %dus pulse width for 1.5 seconds...\n", line_num, pulse);

    // Fire the hardware loops cleanly with absolute zero runtime dependencies
    for (int i = 0; i < 75; i++) {
        write(val_fd, "1", 1);
        usleep(pulse);
        write(val_fd, "0", 1);
        usleep(remainder);
    }

    close(val_fd);
    return 0;
}

