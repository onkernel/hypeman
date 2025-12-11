FROM nvidia/cuda:12.4.1-devel-ubuntu22.04

# The kernel driver version - MUST match exactly for NVML to work
ARG NVIDIA_DRIVER_VERSION=570.86.16

# Install dependencies and Ollama
RUN apt-get update && \
    apt-get install -y curl ca-certificates python3 kmod && \
    curl -fsSL https://ollama.com/install.sh | sh && \
    rm -rf /var/lib/apt/lists/*

# Download NVIDIA driver and extract essential libraries (same version as kernel modules)
# This ensures all userspace libraries match the kernel driver exactly.
# Key libraries:
#   - libcuda.so: CUDA driver API (required for GPU compute)
#   - libnvidia-ml.so: NVIDIA Management Library (GPU monitoring)
#   - libnvidia-ptxjitcompiler.so: PTX JIT compiler (required for CUDA)
#   - libnvidia-nvvm.so: NVIDIA CUDA Compiler driver (required for GPU inference)
RUN curl -fsSL -o /tmp/NVIDIA-Linux-x86_64-${NVIDIA_DRIVER_VERSION}.run \
    https://us.download.nvidia.com/XFree86/Linux-x86_64/${NVIDIA_DRIVER_VERSION}/NVIDIA-Linux-x86_64-${NVIDIA_DRIVER_VERSION}.run && \
    chmod +x /tmp/NVIDIA-Linux-x86_64-${NVIDIA_DRIVER_VERSION}.run && \
    /tmp/NVIDIA-Linux-x86_64-${NVIDIA_DRIVER_VERSION}.run --extract-only --target /tmp/nvidia-driver && \
    # libcuda.so - CUDA driver API
    cp /tmp/nvidia-driver/libcuda.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/ && \
    ln -sf libcuda.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/libcuda.so.1 && \
    ln -sf libcuda.so.1 /usr/lib/x86_64-linux-gnu/libcuda.so && \
    # libnvidia-ml.so - NVML 
    cp /tmp/nvidia-driver/libnvidia-ml.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/ && \
    ln -sf libnvidia-ml.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/libnvidia-ml.so.1 && \
    ln -sf libnvidia-ml.so.1 /usr/lib/x86_64-linux-gnu/libnvidia-ml.so && \
    # libnvidia-ptxjitcompiler.so - PTX JIT
    cp /tmp/nvidia-driver/libnvidia-ptxjitcompiler.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/ && \
    ln -sf libnvidia-ptxjitcompiler.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/libnvidia-ptxjitcompiler.so.1 && \
    ln -sf libnvidia-ptxjitcompiler.so.1 /usr/lib/x86_64-linux-gnu/libnvidia-ptxjitcompiler.so && \
    # libnvidia-nvvm.so - NVVM
    cp /tmp/nvidia-driver/libnvidia-nvvm.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/ && \
    ln -sf libnvidia-nvvm.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/libnvidia-nvvm.so.4 && \
    ln -sf libnvidia-nvvm.so.4 /usr/lib/x86_64-linux-gnu/libnvidia-nvvm.so && \
    # libnvidia-cfg.so - NVIDIA config
    cp /tmp/nvidia-driver/libnvidia-cfg.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/ && \
    ln -sf libnvidia-cfg.so.${NVIDIA_DRIVER_VERSION} /usr/lib/x86_64-linux-gnu/libnvidia-cfg.so.1 && \
    # nvidia-smi binary
    cp /tmp/nvidia-driver/nvidia-smi /usr/bin/ && \
    rm -rf /tmp/nvidia-driver /tmp/NVIDIA-Linux-x86_64-${NVIDIA_DRIVER_VERSION}.run && \
    ldconfig

# Add test scripts
COPY test-nvml.py /usr/local/bin/test-nvml.py
COPY test-cuda.py /usr/local/bin/test-cuda.py
RUN chmod +x /usr/local/bin/test-nvml.py /usr/local/bin/test-cuda.py

# Ensure CUDA and NVIDIA libraries are in the path
ENV LD_LIBRARY_PATH=/usr/local/cuda/lib64:/usr/lib/x86_64-linux-gnu:${LD_LIBRARY_PATH}
ENV PATH=/usr/local/cuda/bin:/usr/bin:${PATH}

EXPOSE 11434
CMD ["ollama", "serve"]

