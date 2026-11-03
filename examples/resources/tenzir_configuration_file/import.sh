# <organization>/<workspace>/<branch>:<path>, the first two each an ID or a
# slug. The first colon ends the branch, which may contain slashes.
terraform import tenzir_configuration_file.ingest acme/soc/main:pipelines/ingest.tql
