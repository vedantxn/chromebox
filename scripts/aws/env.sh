# Shared settings for the AWS scripts. Sourced, not executed.
export AWS_REGION=us-east-1
export CB_AZ=us-east-1a                 # both nodes live in one AZ: faster copies, no cross-AZ data fees
export CB_INSTANCE_TYPE=m8i.large       # 2 vCPU, 8 GiB, supports nested virtualization (ADR 0001)
export CB_KEY_NAME=chromebox
export CB_PUBKEY="$HOME/.ssh/id_ed25519_vedantxn.pub"
export CB_SSH_KEY="$HOME/.ssh/id_ed25519_vedantxn"
export CB_SG_NAME=chromebox-nodes
export CB_TAG=Project=chromebox
# AL2023 with host kernel 6.18, which Firecracker v1.17 supports.
export CB_AMI_PARAM=/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-6.18-x86_64
export CB_AMI_FILE="$(dirname "${BASH_SOURCE[0]}")/ami.txt"
