# XBridge

Bridges a generic USB controller to a virtual input that behaves like a standard Xbox 360 Controller.

This is only for Linux.

Mostly vibecoded for fun and experimentation, the numbers are hardcoded to controllers I have access to so you may need to modify them in the code to make it work for your controller.

## Usage

```sh
go run main.go -grab /dev/input/event18
```

- Replace `/dev/input/event18` with the event number that corresponds to your controller.
- `-grab` will use `EVIOCGRAB` to get exclusive access to the physical controller, meaning other programs won't receive inputs from it, use it if you face duplicate input problems. This doesn't completely hide the controller, programs may still detect the original controller.
- You may have to edit `main.go` and replace the `physicalXXX` constants with the correct numbers for your controller, run `evtest` to find them.

## How It Works

- Opens the `/dev/input/eventX` file and reads `inputEvent`s from it.
- Uses `uinput` through `ioctl` syscalls to create a virtual input and mimics the Xbox 360 vendor/product IDs.
- Forwards the `inputEvent`s from the physical controller to the virtual input after translation.
- If `-grab` is passed, grab the physical controller with `EVIOCGRAB`
- Rumble is currently not implemented