# SOJ - 看似没有特点，实则确实没有特点的开源OJ系统:)
## 项目简介
- SOJ是一款OJ系统!
- 采取wire依赖注入解耦,采取经典三层架构(handle+service+repository)
- 支持ACM模式, 支持比赛和封榜功能

## 技术栈
- Golang
- gin web框架
- gorm ORM框架
- wire 依赖注入
- RabbitMQ 消息队列
- MySQL 关系数据库
- MongoDB 文档数据库
- redis 缓存数据库
- judge0 测评+沙箱 -> 正在尝试切换为codenire
- 腾讯云COS 对象存储
- Docker 容器化


## 部署流程

### 克隆项目
```shell
git clone https://github.com/sparklyi/SOJ.git
```

### 更新配置
```bash
cd SOJ
vi config/config.yaml   # 更新代码配置(如ip 密码等)
vi docker-compose.yaml  # 更新容器配置(如名称 密码等)
vi judge0.conf          # 更新沙箱配置(内存限制等)
```

###  运行
```shell
cd SOJ
docker build -t soj_server:1.0 .
docker-compose up -d 
docker run -d -p 8888:8888 --name soj_server soj_server:1.0
```

## Contribute
欢迎任何形式的贡献


## License
本项目使用[MIT](https://github.com/sparklyi/SOJ?tab=MIT-1-ov-file)许可

## 联系方式
- QQ: 513254687
- VX: sparkyi1026
- Email: sparkyi@foxmail.com

## 分支介绍
- judge0 分支已基本完成，使用judge0沙箱测评   
- codrenire 正在转换为codenire沙箱   
- dev和main分支目前维护judge0   

