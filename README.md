# Chariot
Chariot is a tool for bootstrapping operating systems.  
  
Much inspiration was taken from [xbstrap](https://github.com/managarm/xbstrap) and in most situations [xbstrap](https://github.com/managarm/xbstrap) is probably the more stable and feature-rich option.

## Usage
`chariot [options] [targets]`

## Options
`--config=<file>` overrides the default config file path  
`--cache=<dir>` overrides the default cache path  
`--reset-container` resets the container  
`--verbose` turns on verbose logging (logs stdout)  
`--quiet` turns on quiet logging (no stderr)  
`--threads=<num>` controls the number of parallel threads of execution  

## Config
The config format is due to be documented later when it is more robust. For now refer to the [schema](./chariot-schema.json).

### Temporary Notes for WuX:
**Global Vars:** `$THREADS`, `$PREFIX`, `$ROOT`, `$SOURCE:<id>` if target has the source as a dep.  
**Host Target Vars:** `$BUILD`, `$INSTALL`.  
**Standard Target Vars:** `$BUILD`, `$INSTALL`.  
**Source Modifier Vars:** `$SOURCE`.