#!/bin/bash

# Setup script for deploying chatbot-usach on a fresh Ubuntu VPS
# Run as: bash setup-vps.sh

set -e

echo "🚀 Starting VPS setup for chatbot-usach..."

# Update system
echo "📦 Updating system packages..."
sudo apt-get update
sudo apt-get upgrade -y

# Install Docker
echo "🐳 Installing Docker..."
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com -o get-docker.sh
    sudo sh get-docker.sh
    sudo usermod -aG docker $USER
    rm get-docker.sh
else
    echo "Docker already installed"
fi

# Install Docker Compose
echo "🐳 Installing Docker Compose..."
if ! command -v docker-compose &> /dev/null; then
    sudo curl -L "https://github.com/docker/compose/releases/download/v2.24.0/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
    sudo chmod +x /usr/local/bin/docker-compose
else
    echo "Docker Compose already installed"
fi

# Install Git
echo "📦 Installing Git..."
sudo apt-get install -y git

# Create app directory
echo "📁 Creating application directory..."
sudo mkdir -p /opt/chatbot-usach
sudo chown $USER:$USER /opt/chatbot-usach
cd /opt/chatbot-usach

# Clone repository
echo "📥 Cloning repository..."
if [ ! -d ".git" ]; then
    git clone https://github.com/franciscoparrao/proyecto-chatbot-usach.git .
else
    echo "Repository already exists, pulling latest changes..."
    git pull
fi

# Create .env file
echo "🔧 Creating .env file..."
if [ ! -f ".env" ]; then
    cat > .env << EOF
# Google AI API Key (REQUIRED - get from https://aistudio.google.com/apikey)
GOOGLE_API_KEY=your_google_api_key_here

# MongoDB Configuration
MONGO_URI=mongodb://mongo:27017

# Elasticsearch Configuration  
ES_URL=http://elasticsearch:9200

# Backend Configuration
PORT=8000
CORS_ORIGINS=http://localhost,http://your-domain.com

# Frontend Configuration
VUE_APP_API_URL=http://your-vps-ip:8000
EOF
    echo "⚠️  Please edit /opt/chatbot-usach/.env and add your API keys!"
else
    echo ".env file already exists"
fi

# Create data directories
echo "📁 Creating data directories..."
sudo mkdir -p /opt/chatbot-usach/data/mongodb
sudo mkdir -p /opt/chatbot-usach/data/elasticsearch
sudo chmod -R 777 /opt/chatbot-usach/data

# Setup firewall
echo "🔥 Configuring firewall..."
sudo ufw allow 22/tcp    # SSH
sudo ufw allow 80/tcp    # HTTP
sudo ufw allow 443/tcp   # HTTPS  
sudo ufw allow 8000/tcp  # Backend API
sudo ufw allow 3011/tcp  # Frontend (if needed)
sudo ufw --force enable

# Create systemd service
echo "⚙️  Creating systemd service..."
sudo tee /etc/systemd/system/chatbot-usach.service > /dev/null << EOF
[Unit]
Description=USACH Chatbot Application
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/chatbot-usach
ExecStart=/usr/local/bin/docker-compose -f docker-compose.prod.yml up -d
ExecStop=/usr/local/bin/docker-compose -f docker-compose.prod.yml down
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable chatbot-usach

echo "✅ VPS setup completed!"
echo ""
echo "📋 Next steps:"
echo "1. Edit /opt/chatbot-usach/.env with your API keys"
echo "2. Run: cd /opt/chatbot-usach && sudo docker-compose -f docker-compose.prod.yml up -d"
echo "3. Access your application at http://your-vps-ip:8000"
echo ""
echo "🔧 Useful commands:"
echo "- Start services: sudo systemctl start chatbot-usach"
echo "- Stop services: sudo systemctl stop chatbot-usach"
echo "- View logs: cd /opt/chatbot-usach && docker-compose logs -f"
echo "- Restart services: cd /opt/chatbot-usach && docker-compose restart"