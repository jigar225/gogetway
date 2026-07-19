output "vpc_id" {
  description = "VPC ID"
  value       = module.vpc.vpc_id
}

output "eks_cluster_name" {
  description = "EKS cluster name — use with aws eks update-kubeconfig"
  value       = module.eks.cluster_name
}

output "eks_cluster_endpoint" {
  description = "EKS API server endpoint"
  value       = module.eks.cluster_endpoint
}

output "ecr_repository_url" {
  description = "Push gateway images here"
  value       = aws_ecr_repository.gateway.repository_url
}

output "rds_endpoint" {
  description = "Postgres hostname:port"
  value       = aws_db_instance.this.endpoint
}

output "rds_database_url" {
  description = "Full DATABASE_URL for the gateway (sensitive)"
  sensitive   = true
  value       = "postgres://${var.db_username}:${random_password.db.result}@${aws_db_instance.this.address}:5432/${var.db_name}?sslmode=require"
}

output "redis_endpoint" {
  description = "Redis primary endpoint"
  value       = aws_elasticache_replication_group.this.primary_endpoint_address
}

output "redis_url" {
  description = "REDIS_URL for the gateway"
  value       = "redis://${aws_elasticache_replication_group.this.primary_endpoint_address}:6379"
}

output "configure_kubectl" {
  description = "Run this after apply to talk to the cluster"
  value       = "aws eks update-kubeconfig --region ${var.aws_region} --name ${module.eks.cluster_name} --profile ${var.aws_profile}"
}