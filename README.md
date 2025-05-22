# mockesphome

This is a program to emulate an [ESPHome] device for use with [Home Assistant];
in particular, to emulate a bluetooth proxy.

[ESPHome]: https://esphome.io/
[Home Assistant]: https://www.home-assistant.io/integrations/esphome/

## Building

1. Install [Protocol Buffers] tools
2. Run `make`

[Protocol Buffers]: https://protobuf.dev/getting-started/gotutorial/#compiling-protocol-buffers

## Why?

I have a spare SBC (Raspberry Pi 3) but no extra ESP32 development board.

## License

```
mockesphome
Copyright (C) 2025 Mook

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
```

For the purposes of AGPL remote network interaction, setting `SOURCE_URL` to a
non-empty value during `make` should be considered sufficient.
