# Chariot
Chariot is a simple bootstraping tool for operating systems.

### notes to include in future docs
- Any circular dependency is assumed to be equal, meaning that the target done first is undefined
- 3 types of targest: Host Targets, Source Targets, and Targets
    - Host Target: A target that is installed onto the host (chariot container)
    - Source Target: A target that fetches source (usually source code) and configures it
    - Standard Target: A target that is installed into the sysroot