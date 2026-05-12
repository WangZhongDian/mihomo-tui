#!/bin/bash
set -e

# ============================================================
# mihomo-tui 快速安装脚本
# 用法: curl -fsSL .../install.sh | sudo bash
# 环境变量:
#   DOWNLOAD_URL     裸二进制完整下载 URL（优先级最高）
#   DOWNLOAD_URL_GZ  gzip 压缩版完整下载 URL
#   REPO             GitHub 仓库地址 (默认: mihomo-tui/mihomo-tui)
#   INSTALL_DIR      安装目录 (默认: /usr/local/bin)
#   TMP_DIR          临时目录 (默认: /tmp)
# ============================================================

REPO="${REPO:-mihomo-tui/mihomo-tui}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
TMP_DIR="${TMP_DIR:-/tmp}"

# ---------- 1. 权限检查 ----------
if [ "$(id -u)" -ne 0 ]; then
    echo "[错误] 需要 root 权限运行，请使用 sudo bash install.sh" >&2
    exit 1
fi

# ---------- 2. 平台检测 ----------
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)           ARCH="amd64" ;;
    aarch64|arm64)    ARCH="arm64" ;;
    armv7l)           ARCH="armv7" ;;
    i386|i686)        ARCH="386"   ;;
    *)                ARCH="${ARCH}" ;;
esac

if [ "$OS" != "linux" ]; then
    echo "[错误] 当前仅支持 Linux 系统 (检测到: ${OS})" >&2
    exit 1
fi

# 用户可传入完整的下载 URL；未传入时基于 REPO 拼接默认 GitHub 地址
BINARY_NAME="mihomo-tui-${OS}-${ARCH}"
: "${DOWNLOAD_URL:=https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}}"
: "${DOWNLOAD_URL_GZ:=https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}.gz}"

TMP_BIN="${TMP_DIR}/mihomo-tui-download-$$"

# ---------- 3. 下载二进制 ----------
echo ">>> 检测平台: ${OS}/${ARCH}"
echo ">>> 下载 ..."

# 先尝试直接下载裸二进制
if curl -fsSL -o "${TMP_BIN}" "${DOWNLOAD_URL}" 2>/dev/null; then
    echo ">>> 下载完成 (裸二进制)"
# 再尝试下载 gzip 压缩版本并解压
elif curl -fsSL -o "${TMP_BIN}.gz" "${DOWNLOAD_URL_GZ}" 2>/dev/null; then
    echo ">>> 下载完成 (gzip 压缩)，正在解压..."
    gunzip -f "${TMP_BIN}.gz"
else
    echo "[错误] 下载失败，请检查网络或下载地址:" >&2
    echo "       ${DOWNLOAD_URL}" >&2
    echo "       ${DOWNLOAD_URL_GZ}" >&2
    rm -f "${TMP_BIN}" "${TMP_BIN}.gz"
    exit 1
fi

chmod +x "${TMP_BIN}"

# ---------- 4. 安装服务 ----------
echo ">>> 安装 systemd 服务..."
"${TMP_BIN}" install_service

# ---------- 5. 清理 ----------
echo ">>> 清理临时文件..."
rm -f "${TMP_BIN}" "${TMP_BIN}.gz"

# ---------- 6. 完成 ----------
echo ""
echo "✅ mihomo-tui 安装完成"
echo "   可执行文件: ${INSTALL_DIR}/mihomo-tui"
echo "   配置目录  : /var/lib/mihomo-tui (root) 或 ~/.config/mihomo-tui"
echo ""
echo "常用命令:"
echo "   sudo systemctl status  mihomo-tui"
echo "   sudo systemctl stop    mihomo-tui"
echo "   sudo systemctl restart mihomo-tui"
echo "   mihomo-tui             # 启动 TUI 客户端"
