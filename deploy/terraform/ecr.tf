# -----------------------------------------------------------------------------
# ECR — private Docker image registry (this is NOT ECS)
# Flow: docker build → docker push ECR → EKS pods pull image
# -----------------------------------------------------------------------------

resource "aws_ecr_repository" "gateway" {
  name                 = "${local.name}/gateway"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = {
    Name = "${local.name}-gateway"
  }
}

resource "aws_ecr_lifecycle_policy" "gateway" {
  repository = aws_ecr_repository.gateway.name

  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep last 10 images"
      selection = {
        tagStatus   = "any"
        countType   = "imageCountMoreThan"
        countNumber = 10
      }
      action = {
        type = "expire"
      }
    }]
  })
}