#!/bin/bash
cd /root || exit 1
if [ ! -d WSL2-Linux-Kernel ]; then
  echo "=== CLONE_START ==="
  git clone --depth 1 --branch linux-msft-wsl-6.18.y https://github.com/microsoft/WSL2-Linux-Kernel.git || { echo "CLONE_FAILED"; exit 1; }
else
  echo "=== REUSE_EXISTING ==="
fi
cd WSL2-Linux-Kernel
cp Microsoft/config-wsl .config
./scripts/config --enable USB_SUPPORT
./scripts/config --enable USB
./scripts/config --enable BT
./scripts/config --enable BT_HCIBTUSB
./scripts/config --enable BT_HCIBTUSB_MTK
./scripts/config --enable BT_HCIBTUSB_REALTEK
./scripts/config --enable BT_HIDP
./scripts/config --enable FW_LOADER_COMPRESS_ZSTD
make olddefconfig
sed -i 's/^CONFIG_FW_LOADER_USER_HELPER=y/# CONFIG_FW_LOADER_USER_HELPER is not set/' .config
./scripts/config --set-str EXTRA_FIRMWARE "mediatek/BT_RAM_CODE_MT7922_1_1_hdr.bin"
./scripts/config --set-str EXTRA_FIRMWARE_DIR "/lib/firmware"
echo "=== CONFIG_CHECK ==="
grep -E "CONFIG_USB_SUPPORT=|CONFIG_USB=|CONFIG_BT=|CONFIG_BT_HCIBTUSB=|CONFIG_BT_HCIBTUSB_MTK=|FW_LOADER_USER_HELPER" .config
if ! grep -q "CONFIG_BT_HCIBTUSB=y" .config; then echo "=== WARN: BT_HCIBTUSB not builtin ==="; fi
echo "=== BUILD_START ==="
make -j"$(nproc)" > /tmp/kernel_build.log 2>&1
RC=$?
echo "=== BUILD_DONE rc=$RC ==="
if [ -f arch/x86/boot/bzImage ]; then
  cp arch/x86/boot/bzImage /root/bzImage
  echo "=== BZIMAGE_OK ==="
  ls -la /root/bzImage
else
  echo "=== NO_BZIMAGE ==="
  tail -40 /tmp/kernel_build.log
fi
