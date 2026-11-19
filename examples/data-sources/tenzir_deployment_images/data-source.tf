data "tenzir_deployment_images" "all" {}

output "default_image" {
  value = data.tenzir_deployment_images.all.default
}
