# Deployment en VPS - Guía Completa

## 1. Obtener un VPS

### Opciones recomendadas:
- **DigitalOcean**: $6/mes (1GB RAM, 25GB SSD)
- **Linode**: $5/mes (1GB RAM, 25GB SSD)
- **Hetzner**: €4.51/mes (2GB RAM, 20GB SSD)
- **AWS EC2**: t2.micro gratis por 12 meses

### Requisitos mínimos:
- 2GB RAM (recomendado 4GB)
- 20GB almacenamiento
- Ubuntu 22.04 LTS

## 2. Configuración Inicial del VPS

```bash
# Conectarte al VPS
ssh root@tu-vps-ip

# Crear usuario no-root (recomendado)
adduser tuusuario
usermod -aG sudo tuusuario
su - tuusuario
```

## 3. Deployment Automático

```bash
# Descargar y ejecutar script de setup
wget https://raw.githubusercontent.com/franciscoparrao/proyecto-chatbot-usach/main/deployment/setup-vps.sh
chmod +x setup-vps.sh
./setup-vps.sh
```

## 4. Configuración Manual (si prefieres)

### Instalar dependencias:
```bash
# Actualizar sistema
sudo apt update && sudo apt upgrade -y

# Instalar Docker
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER

# Instalar Docker Compose
sudo curl -L "https://github.com/docker/compose/releases/download/v2.24.0/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose

# Instalar Git
sudo apt install -y git
```

### Clonar y configurar:
```bash
# Clonar repositorio
cd /opt
sudo git clone https://github.com/franciscoparrao/proyecto-chatbot-usach.git
cd proyecto-chatbot-usach

# Crear archivo .env
cp .env.example .env
nano .env  # Editar con tus API keys
```

### Iniciar servicios:
```bash
# Iniciar con Docker Compose
sudo docker-compose -f docker-compose.prod.yml up -d

# Ver logs
sudo docker-compose logs -f
```

## 5. Migrar Datos (si tienes datos locales)

```bash
# Desde tu máquina LOCAL
cd scripts

# Migrar MongoDB
go run migrate_to_railway.go "mongodb://tu-vps-ip:27017"

# Configurar Elasticsearch
go run setup_elasticsearch_railway.go "http://tu-vps-ip:9200"

# Reindexar datos
export MONGO_URI="mongodb://tu-vps-ip:27017"
export ES_URL="http://tu-vps-ip:9200"
export GOOGLE_API_KEY="tu-api-key"
go run reindex_to_elasticsearch.go
```

## 6. Configurar Dominio (Opcional)

### Con Nginx y Let's Encrypt:
```bash
# Instalar Certbot
sudo apt install certbot python3-certbot-nginx

# Obtener certificado SSL
sudo certbot --nginx -d tu-dominio.com
```

## 7. Monitoreo y Mantenimiento

### Comandos útiles:
```bash
# Ver estado de servicios
sudo docker-compose ps

# Reiniciar servicios
sudo docker-compose restart

# Ver logs en tiempo real
sudo docker-compose logs -f backend

# Backup de MongoDB
docker exec chatbot-mongo mongodump --out /backup

# Actualizar aplicación
git pull
sudo docker-compose build
sudo docker-compose up -d
```

### Monitoreo básico:
```bash
# Ver uso de recursos
htop

# Ver espacio en disco
df -h

# Ver logs del sistema
sudo journalctl -f
```

## 8. Seguridad

### Configurar firewall:
```bash
sudo ufw allow 22/tcp    # SSH
sudo ufw allow 80/tcp    # HTTP
sudo ufw allow 443/tcp   # HTTPS
sudo ufw enable
```

### Fail2ban (protección contra ataques):
```bash
sudo apt install fail2ban
sudo systemctl enable fail2ban
```

## 9. Troubleshooting

### Si MongoDB no inicia:
```bash
# Verificar permisos
sudo chown -R 999:999 ./data/mongodb
```

### Si Elasticsearch no inicia:
```bash
# Aumentar memoria virtual
sudo sysctl -w vm.max_map_count=262144
echo "vm.max_map_count=262144" | sudo tee -a /etc/sysctl.conf
```

### Si el backend no conecta:
```bash
# Verificar variables de entorno
docker exec chatbot-backend env

# Verificar conectividad
docker exec chatbot-backend ping mongo
```

## 10. Costos Estimados

- VPS básico: $5-10/mes
- Dominio: $10-15/año
- Total: ~$7-12/mes

¡Listo! Tu chatbot está corriendo en producción 🚀