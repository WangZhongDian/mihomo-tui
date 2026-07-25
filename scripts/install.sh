#!/bin/bash
set -e

# ============================================================
# mihomo-tui 快速安装脚本
# GitHub: https://github.com/WangZhongDian/mihomo-tui
#
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/WangZhongDian/mihomo-tui/main/scripts/install.sh | sudo bash
#   curl -fsSL ... | sudo bash -s -- -v v0.1.0
#
# 参数:
#   -v VERSION   指定版本号（默认: latest）
#   -d DIR       指定安装目录（默认: /usr/local/bin）
#
# 环境变量:
#   DOWNLOAD_URL     裸二进制完整下载 URL（优先级最高）
#   DOWNLOAD_URL_GZ  gzip 压缩版完整下载 URL
#   REPO             GitHub 仓库地址 (默认: WangZhongDian/mihomo-tui)
#   INSTALL_DIR      安装目录 (默认: /usr/local/bin)
#   TMP_DIR          临时目录 (默认: /tmp)
# ============================================================

REPO="${REPO:-WangZhongDian/mihomo-tui}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
TMP_DIR="${TMP_DIR:-/tmp}"
VERSION="latest"

# ---------- 参数解析 ----------
while getopts "v:d:h" opt; do
    case $opt in
        v) VERSION="$OPTARG" ;;
        d) INSTALL_DIR="$OPTARG" ;;
        h)
            echo "用法: install.sh [-v VERSION] [-d DIR]"
            echo "  -v VERSION   指定版本号（默认: latest）"
            echo "  -d DIR       指定安装目录（默认: /usr/local/bin）"
            exit 0
            ;;
        *) exit 1 ;;
    esac
done

# ---------- 权限检查 ----------
if [ "$(id -u)" -ne 0 ]; then
    echo "[错误] 需要 root 权限运行，请使用 sudo bash install.sh" >&2
    exit 1
fi

# ---------- 平台检测 ----------
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

# ---------- 下载地址 ----------
BINARY_NAME="mihomo-tui-${OS}-${ARCH}"

if [ -n "${DOWNLOAD_URL:-}" ]; then
    true  # 使用用户传入的完整 URL
elif [ "$VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}"
    DOWNLOAD_URL_GZ="https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}.gz"
else
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY_NAME}"
    DOWNLOAD_URL_GZ="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY_NAME}.gz"
fi

TMP_BIN="${TMP_DIR}/mihomo-tui-download-$$"

# ---------- 下载二进制 ----------
echo ">>> 检测平台: ${OS}/${ARCH}"
echo ">>> 目标版本: ${VERSION}"
echo ">>> 下载地址: ${DOWNLOAD_URL}"

# 先尝试直接下载裸二进制
if curl -fsSL --connect-timeout 30 --max-time 120 -o "${TMP_BIN}" "${DOWNLOAD_URL}" 2>/dev/null; then
    echo ">>> 下载完成 (裸二进制)"
# 再尝试下载 gzip 压缩版本并解压
elif curl -fsSL --connect-timeout 30 --max-time 120 -o "${TMP_BIN}.gz" "${DOWNLOAD_URL_GZ}" 2>/dev/null; then
    echo ">>> 下载完成 (gzip 压缩)，正在解压..."
    gunzip -f "${TMP_BIN}.gz"
    mv "${TMP_BIN}" "${TMP_BIN}"  # 保持变量名一致
else
    echo "[错误] 下载失败，请检查网络或版本号是否正确:" >&2
    echo "       ${DOWNLOAD_URL}" >&2
    echo "       ${DOWNLOAD_URL_GZ}" >&2
    rm -f "${TMP_BIN}" "${TMP_BIN}.gz"
    exit 1
fi

chmod +x "${TMP_BIN}"

# ---------- 检查已有服务与内核状态（必须在 stop 前快照内核）----------
# 注意：先下载完再停止，利用旧版服务的代理能力访问 GitHub
SERVICE_NAME="mihomo-tui"
KERNEL_WAS_RUNNING=false

if systemctl list-unit-files --type=service 2>/dev/null | grep -q "^${SERVICE_NAME}\.service"; then
    if systemctl is-active --quiet "${SERVICE_NAME}"; then
        echo ">>> 检测到 ${SERVICE_NAME} 服务正在运行"
        # 通过 root daemon Unix socket 探测 mihomo 内核（须在 stop 前完成）
        SOCK="/var/run/mihomo-tui/daemon.sock"
        if [ -S "${SOCK}" ] && command -v curl >/dev/null 2>&1; then
            RESP="$(curl -fsS --max-time 3 --unix-socket "${SOCK}" "http://daemon/api/v1/mihomo/status" 2>/dev/null || true)"
            # 兼容 compact JSON 与带空格的编码
            if printf '%s' "${RESP}" | grep -Eq '"running"[[:space:]]*:[[:space:]]*true'; then
                KERNEL_WAS_RUNNING=true
            fi
        fi
        if [ "${KERNEL_WAS_RUNNING}" = true ]; then
            echo ">>> 安装前 mihomo 内核处于运行状态，完成后将自动恢复"
        else
            echo ">>> 安装前 mihomo 内核未运行（或无法探测），完成后不自动启动内核"
        fi
        echo ">>> 停止 ${SERVICE_NAME} 服务..."
        systemctl stop "${SERVICE_NAME}"
    fi
fi

# ---------- 安装二进制 ----------
echo ">>> 安装到 ${INSTALL_DIR} ..."
# 原子替换：避免 ETXTBSY（旧进程仍映射可执行文件 inode）
mkdir -p "${INSTALL_DIR}"
STAGE_BIN="${INSTALL_DIR}/.mihomo-tui.new.$$"
cp "${TMP_BIN}" "${STAGE_BIN}"
chmod 755 "${STAGE_BIN}"
mv -f "${STAGE_BIN}" "${INSTALL_DIR}/mihomo-tui"

# ---------- 安装/更新服务（内含启动服务 + 按快照恢复内核）----------
echo ">>> 安装 systemd 服务..."
# 显式传入快照：install.sh 在 stop 后调用，install_service 无法再自行探测旧状态
if [ "${KERNEL_WAS_RUNNING}" = true ]; then
    export MIHOMO_TUI_KERNEL_WAS_RUNNING=1
    "${INSTALL_DIR}/mihomo-tui" install_service --kernel-was-running=true
else
    export MIHOMO_TUI_KERNEL_WAS_RUNNING=0
    "${INSTALL_DIR}/mihomo-tui" install_service --kernel-was-running=false
fi

# ---------- 清理 ----------
echo ">>> 清理临时文件..."
rm -f "${TMP_BIN}" "${TMP_BIN}.gz"

# ---------- 完成 ----------
echo ""
echo "✅ mihomo-tui 安装完成"
echo "   版本     : $(mihomo-tui version 2>/dev/null || echo 'N/A')"
echo "   可执行文件: ${INSTALL_DIR}/mihomo-tui"
echo "   配置目录  : /var/lib/mihomo-tui (root) 或 ~/.config/mihomo-tui"
echo ""
echo "常用命令:"
echo "   sudo systemctl status  mihomo-tui"
echo "   sudo systemctl stop    mihomo-tui"
echo "   sudo systemctl restart mihomo-tui"
echo "   mihomo-tui             # 启动 TUI 客户端"
