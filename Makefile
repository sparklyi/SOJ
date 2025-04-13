# 定义变量
DB_NAME = mydb
SQL_FILE = init.sql
MYSQL_USER = root
MYSQL_PASSWORD = password  # 根据你的环境修改数据库的密码
MYSQL_HOST = localhost

# 默认目标
all: check_db init_db

# 检查数据库是否存在
check_db:
	@echo "Checking if the database $(DB_NAME) exists..."
	@mysql -u $(MYSQL_USER) -p$(MYSQL_PASSWORD) -h $(MYSQL_HOST) -e "USE $(DB_NAME);" > /dev/null 2>&1 && \
		echo "Database $(DB_NAME) already exists, skipping initialization." || \
		make init_db

# 初始化数据库并导入数据
init_db:
	@echo "Creating database $(DB_NAME) and importing $(SQL_FILE)..."
	@mysql -u $(MYSQL_USER) -p$(MYSQL_PASSWORD) -h $(MYSQL_HOST) -e "CREATE DATABASE IF NOT EXISTS $(DB_NAME);"
	@mysql -u $(MYSQL_USER) -p$(MYSQL_PASSWORD) -h $(MYSQL_HOST) $(DB_NAME) < $(SQL_FILE)
	@echo "Database $(DB_NAME) initialized and data imported successfully!"

# 清理数据库
clean:
	@echo "Dropping database $(DB_NAME)..."
	@mysql -u $(MYSQL_USER) -p$(MYSQL_PASSWORD) -h $(MYSQL_HOST) -e "DROP DATABASE IF EXISTS $(DB_NAME);"
	@echo "Database $(DB_NAME) dropped!"

.PHONY: all check_db init_db clean
