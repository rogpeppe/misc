#PiGlow

[![GoDoc](https://godoc.org/github.com/rogpeppe/misc/piglow?status.svg)](https://godoc.org/github.com/rogpeppe/misc/piglow)

[Manufacturer info](https://shop.pimoroni.com/products/piglow)

![PiGlow](https://cdn.shopify.com/s/files/1/0174/1800/products/PiGlow-3_1024x1024.gif?v=1424952533)

This package provides an alternative API to that provided by https://godoc.org/github.com/goiot/devices/piglow
which aims to make it easier to use without hard-coding maps from LED number to position.

The PiGlow is a small add on board for the Raspberry Pi that provides 18 individually controllable LEDs. This board uses the
SN3218 8-bit 18-channel PWM chip to drive 18 surface mount LEDs. Communication is done via I2C over the GPIO header with a bus address of 0x54.
Each LED can be set to a PWM value of between 0 and 255.

##Datasheet:

* [SN3218 Datasheet](https://github.com/pimoroni/piglow/raw/master/sn3218-datasheet.pdf)
