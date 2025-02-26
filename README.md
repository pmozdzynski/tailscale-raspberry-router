# 🛠 Tailscale Raspberry Router

**Tailscale Raspberry Router** is a project that turns a Raspberry Pi or any Linux device into a **Tailscale-powered VPN Router**. It enables devices on a local network to route their traffic through **Tailscale exit nodes**, including **Mullvad VPN nodes** and **Private Exit Nodes**.

![VPN Router Dashboard](Dashboard.png)

This project allows:
- **Seamless integration with Tailscale**
- **Easy switching between direct internet & Tailscale exit nodes**
- **Running on Raspberry Pi & Linux Desktops**
- **Multi-interface support (e.g., `eth0`, `wlan0`, `wlan1`)**
- **Web Dashboard for controlling exit nodes** 🌍
- **Support for Private Exit Nodes for self-hosted VPN routing**
- **Recommends `dnsmasq` for DHCP on a second interface if needed**
- **Creates a VPN router for secure traffic routing over Tailscale**
- **Useful for securing devices that require a stable VPN connection**

---

## **🚀 Features**
✅ **Use a Raspberry Pi or any Linux device as a Tailscale-powered VPN router**  
✅ **Dynamic detection of available exit nodes (including Mullvad VPN & Private Exit Nodes)**  
✅ **Real-time web dashboard to switch between exit nodes**  
✅ **Persistent mode saving (restores the last used exit node on startup)**  
✅ **Auto-detects and configures the primary network interface (e.g., `eth0`, `wlan0`)**  
✅ **Runs as a systemd service for automatic startup**  
✅ **Supports two network interfaces (one for WAN, one for LAN clients) but works with just one**  
✅ **Allows external DHCP configuration (e.g., `dnsmasq` for local clients)**  
✅ **Supports `friendly-names.json` to assign friendly names to exit nodes**  
✅ **Acts as a VPN router for devices needing secure and consistent VPN connections**  

---

## **💻 Installation & Setup**

### **1️⃣ Install & Authenticate Tailscale**
```sh
curl -fsSL https://tailscale.com/install.sh | sh
```
Then, authenticate your device:
```sh
sudo tailscale up
```

### **2️⃣ Clone the Repository & Compile for Your Platform**
```sh
git clone https://github.com/pmozdzynski/tailscale-raspberry-router.git
cd tailscale-raspberry-router
```
#### **Cross-Platform Build for Raspberry Pi and Linux Devices**
To build for different architectures, use the appropriate `GOARCH` and `GOARM` flags:
```sh
# For Raspberry Pi 4 (ARM64)
GOOS=linux GOARCH=arm64 go build -o tailscale-raspberry-router main.go

# For Raspberry Pi 3/2/1 (32-bit ARM)
GOOS=linux GOARCH=arm GOARM=7 go build -o tailscale-raspberry-router main.go

# For x86_64 Linux Desktops
GOOS=linux GOARCH=amd64 go build -o tailscale-raspberry-router main.go
```
Move the compiled binary:
```sh
sudo mkdir -p /opt/tailscale-raspberry-router/templates/
sudo mv tailscale-raspberry-router /opt/tailscale-raspberry-router/
sudo mv templates/* /opt/tailscale-raspberry-router/templates/
```

### **3️⃣ Set Up the Systemd Service**
```sh
sudo nano /etc/systemd/system/tailscale-router.service
```
Paste the following:
```
[Unit]
Description=VPN Router Service
After=network-online.target tailscaled.service systemd-networkd-wait-online.service
Wants=network-online.target tailscaled.service systemd-networkd-wait-online.service
Before=shutdown.target

[Service]
Type=idle
ExecStartPre=/bin/sh -c 'echo "Starting Tailscale Router service, please wait..." | systemd-cat -t tailscale-router'; /bin/sleep 15
ExecStart=/opt/tailscale-raspberry-router/tailscale-raspberry-router
WorkingDirectory=/opt/tailscale-raspberry-router/
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
```
Enable and start:
```sh
sudo systemctl daemon-reload
sudo systemctl enable tailscale-router.service
sudo systemctl start tailscale-router.service
```
> **Note:** The service takes approximately **15-30 seconds** to start due to the sleep delay and tailscale/interfaces detection. Please wait before attempting to access the Web UI.

Then, access the Web UI at: 
```
http://<your-device-ip>:5000
```
Use this interface to log in and select an **exit node**.

Please note: You must have at least 1 active Exit node on your account to see anything it in router web-UI. You can check them here https://login.tailscale.com/admin/machines  
If you do not have any own Exit nodes, you can get paid Mullvad addon from https://login.tailscale.com/admin/settings/general

---

## **🔒 Using an Exit Node for LAN Clients**

This device cannot act as an exit node but can route all connected LAN clients through a Tailscale Exit Node. It functions as a router for LAN clients, forwarding their traffic through an external Tailscale Exit Node rather than terminating VPN connections itself.

### **Selecting an Exit Node via Web Interface**

1️⃣ Open the Web Dashboard:
```
http://<your-device-ip>:5000
```
2️⃣ Choose an available **exit node** from the list.


3️⃣ Click **Apply** to route all LAN traffic through the selected exit node.

### **Verify Connection**
To ensure that LAN clients are routing traffic through the selected exit node, run the following command on any connected client:
```sh
curl ifconfig.me
```
The output should match the external IP of the selected exit node.

---

## **🐝 Cross-Platform Compatibility**
This project supports multiple architectures, including:
- **Raspberry Pi 4 (ARM64)**
- **Raspberry Pi 3/2/1 (ARM32 with GOARM=7)**
- **x86_64 Linux Desktops**
- **Other Linux-based single-board computers**

Ensure you compile the binary for the correct architecture before deployment.

---

## **🌍 License**
```
MIT License

Copyright (c) 2025 P.S. Mozdzynski

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## **💬 Need Help?**
Post an issue in the GitHub repository or ask in the **Tailscale community forums**!

