# -----------------------------------------------------------------------------
# Networking (VPC)
# Why: EKS, RDS, and Redis need private network space. Public subnets hold the
# load balancer + NAT; private subnets hold pods and data stores.
# -----------------------------------------------------------------------------

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  name = "${var.project_name}-${var.environment}"
  azs  = slice(data.aws_availability_zones.available.names, 0, 2)

  # 10.0.0.0/16 split: public 10.0.0.0/24 + 10.0.1.0/24, private 10.0.10.0/24 + 10.0.11.0/24
  public_subnets  = [for i, az in local.azs : cidrsubnet(var.vpc_cidr, 8, i)]
  private_subnets = [for i, az in local.azs : cidrsubnet(var.vpc_cidr, 8, i + 10)]
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.0"

  name = local.name
  cidr = var.vpc_cidr

  azs             = local.azs
  public_subnets  = local.public_subnets
  private_subnets = local.private_subnets

  # One NAT Gateway = cheaper (~$32/mo). Multi-AZ NAT = more HA, more cost.
  enable_nat_gateway = true
  single_nat_gateway = true

  enable_dns_hostnames = true
  enable_dns_support   = true

  # EKS needs these tags so the AWS Load Balancer Controller / VPC CNI
  # know which subnets are public vs private.
  public_subnet_tags = {
    "kubernetes.io/role/elb"                    = "1"
    "kubernetes.io/cluster/${local.name}"       = "shared"
  }

  private_subnet_tags = {
    "kubernetes.io/role/internal-elb"           = "1"
    "kubernetes.io/cluster/${local.name}"       = "shared"
  }
}