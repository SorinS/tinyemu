bin/temu.darwin-arm64.bin -machine x86 -m 512 -kernel bin/bzImage-qemux86.bin -initrd bin/initramfs.new -append "console=ttyS0,115200 earlyprintk=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr lpj=100000 tsc=reliable rdinit=/myshell"

