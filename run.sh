bin/tinyemu -m 128 -bios testdata/boot/bbl64.bin -kernel testdata/boot/kernel-riscv64.bin -drive testdata/boot/root-riscv64.bin -append "console=hvc0 root=/dev/vda rw"
