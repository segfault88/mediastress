# Mediastress

This is a go program I threw together at work in 2014 in Go to use a local FreeSwitch server to stress another mediaserver.

Essentially it uses the FS command socket interface to instruct it to dial a number, play an audio file then hang up.

Not a particularly good example of Go code, or using Freeswitch, but may be useful to somebody somehere...