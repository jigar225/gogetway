# -----------------------------------------------------------------------------
# RDS Postgres — durable usage logs + virtual keys (PRD data model)
# Private subnet only: not reachable from the public internet.
# -----------------------------------------------------------------------------

resource "random_password" "db" {
  length  = 24
  special = false # avoid URL-encoding pain in connection strings
}

resource "aws_db_subnet_group" "this" {
  name       = "${local.name}-db"
  subnet_ids = module.vpc.private_subnets

  tags = {
    Name = "${local.name}-db"
  }
}

resource "aws_security_group" "rds" {
  name_prefix = "${local.name}-rds-"
  description = "Postgres access from EKS nodes only"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "Postgres from EKS node SG"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [module.eks.node_security_group_id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${local.name}-rds"
  }
}

resource "aws_db_instance" "this" {
  identifier = "${local.name}-postgres"

  engine               = "postgres"
  engine_version       = "16"
  instance_class       = var.db_instance_class
  allocated_storage    = 20
  max_allocated_storage = 50
  storage_type         = "gp3"

  db_name  = var.db_name
  username = var.db_username
  password = random_password.db.result

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  # Learning defaults: single-AZ, skip final snapshot for easy destroy.
  # Flip these for real prod.
  multi_az                = false
  publicly_accessible     = false
  skip_final_snapshot     = true
  deletion_protection     = false
  backup_retention_period = 0

  tags = {
    Name = "${local.name}-postgres"
  }
}