# -----------------------------------------------------------------------------
# EKS — managed Kubernetes control plane + worker nodes
# Why: matches PRD Phase 4/7 — multi-replica gateway with HPA + pod failover.
# Cost note: EKS control plane alone is ~$73/month. Destroy when not demoing.
# -----------------------------------------------------------------------------

module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.0"

  cluster_name    = local.name
  cluster_version = var.eks_cluster_version

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnets

  # Public endpoint so your laptop kubectl works without a VPN (fine for learning).
  # Production often uses private-only + bastion/VPN.
  cluster_endpoint_public_access  = true
  cluster_endpoint_private_access = true

  # Gives the AWS account admin access to the cluster (needed for kubectl).
  enable_cluster_creator_admin_permissions = true

  eks_managed_node_groups = {
    gateway = {
      name           = "gateway"
      instance_types = var.eks_node_instance_types

      min_size     = var.eks_min_size
      max_size     = var.eks_max_size
      desired_size = var.eks_desired_size

      # Spot is cheaper for learning; switch to ON_DEMAND for interview demos
      # if you need stable nodes.
      capacity_type = "ON_DEMAND"

      labels = {
        workload = "gogate"
      }
    }
  }

  # Install core addons EKS expects.
  cluster_addons = {
    coredns                = {}
    kube-proxy             = {}
    vpc-cni                = {}
    eks-pod-identity-agent = {}
  }
}