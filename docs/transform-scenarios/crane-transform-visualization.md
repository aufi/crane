# Crane Transform - Visual Guide

This document contains visualizations to explain the `crane transform` command flow including multiple stages and kustomize.

## 1. Overall Migration Workflow

```mermaid
flowchart LR
    A[Source Cluster] -->|crane export| B[export/resources/]
    B -->|crane transform| C[transform/stages/]
    C -->|crane apply| D[output/]
    D -->|kubectl apply| E[Target Cluster]
    
    style A fill:#e1f5ff
    style B fill:#fff4e1
    style C fill:#e8f5e9
    style D fill:#f3e5f5
    style E fill:#e1f5ff
```

## 2. Multi-Stage Pipeline - Sequential Processing

```mermaid
flowchart TB
    subgraph Input["📥 Input"]
        EXP[export/resources/<br/>RAW YAML with metadata]
    end
    
    subgraph Stage1["Stage 1: 10_KubernetesPlugin"]
        S1R[resources/]
        S1P[patches/]
        S1K[kustomization.yaml]
        S1R -.-> S1P
        S1P -.-> S1K
    end
    
    subgraph Work1[".work/10_KubernetesPlugin/"]
        W1I[input/<br/>copy from export/]
        W1O[output/<br/>❗ Materialized YAML]
    end
    
    subgraph Stage2["Stage 2: 20_OpenshiftPlugin"]
        S2R[resources/]
        S2P[patches/]
        S2K[kustomization.yaml]
        S2R -.-> S2P
        S2P -.-> S2K
    end
    
    subgraph Work2[".work/20_OpenshiftPlugin/"]
        W2I[input/<br/>copy from stage 1 output]
        W2O[output/<br/>❗ Materialized YAML]
    end
    
    subgraph Stage3["Stage 3: 50_CustomEdits"]
        S3R[resources/]
        S3P[patches/]
        S3K[kustomization.yaml]
        S3R -.-> S3P
        S3P -.-> S3K
    end
    
    subgraph Work3[".work/50_CustomEdits/"]
        W3I[input/<br/>copy from stage 2 output]
        W3O[output/<br/>❗ Materialized YAML]
    end
    
    subgraph Output["📤 Output"]
        OUT[output/<br/>Final YAML for target cluster]
    end
    
    EXP -->|reads| Stage1
    Stage1 -->|kubectl kustomize| W1O
    W1O -->|copies to| Stage2
    Stage2 -->|kubectl kustomize| W2O
    W2O -->|copies to| Stage3
    Stage3 -->|kubectl kustomize| W3O
    W3O -->|crane apply| OUT
    
    style Input fill:#fff4e1
    style Stage1 fill:#e3f2fd
    style Work1 fill:#f1f8e9
    style Stage2 fill:#e3f2fd
    style Work2 fill:#f1f8e9
    style Stage3 fill:#e3f2fd
    style Work3 fill:#f1f8e9
    style Output fill:#f3e5f5
    style W1O fill:#ffeb3b,stroke:#f57c00,stroke-width:3px
    style W2O fill:#ffeb3b,stroke:#f57c00,stroke-width:3px
    style W3O fill:#ffeb3b,stroke:#f57c00,stroke-width:3px
```

**Key Concept**: Each stage sees the **fully materialized output** from the previous stage, not raw patches!

## 3. Stage Types - Plugin vs Pass-Through

```mermaid
flowchart TB
    subgraph PluginStage["🔄 Plugin Stage<br/>(ends with 'Plugin')"]
        direction TB
        PS1[10_KubernetesPlugin]
        PS2[20_OpenshiftPlugin]
        PSF["✅ Auto-regeneration<br/>✅ Patches generated automatically<br/>❌ Cannot manually edit<br/>❌ --force not needed"]
    end
    
    subgraph PassStage["✏️ Pass-Through Stage<br/>(does not end with 'Plugin')"]
        direction TB
        PT1[50_CustomEdits]
        PT2[90_FinalTweaks]
        PTF["✅ Protected from overwrite<br/>✅ Manual editing allowed<br/>❌ Requires --force<br/>❌ Patches not generated"]
    end
    
    style PluginStage fill:#e3f2fd
    style PassStage fill:#fff3e0
```

## 4. Stage Directory Structure

```mermaid
graph TB
    subgraph Stage["transform/10_KubernetesPlugin/"]
        direction TB
        
        subgraph Resources["resources/"]
            R1[Deployment_apps_v1_default_myapp.yaml]
            R2[Service__v1_default_myapp.yaml]
            R3[ConfigMap__v1_default_myapp-config.yaml]
        end
        
        subgraph Patches["patches/"]
            P1[default--apps-v1--Deployment--myapp.patch.yaml]
            P2[default--v1--Service--myapp.patch.yaml]
        end
        
        K[kustomization.yaml]
    end
    
    K -.->|references| Resources
    K -.->|applies| Patches
    
    style Stage fill:#e8f5e9
    style Resources fill:#fff9c4
    style Patches fill:#ffe0b2
    style K fill:#f8bbd0
```

## 5. Kustomization.yaml - Key Components

```mermaid
flowchart TB
    subgraph Kust["kustomization.yaml"]
        direction TB
        
        API["apiVersion: kustomize.config.k8s.io/v1beta1<br/>kind: Kustomization"]
        
        NS["namespace: migrated-app"]
        
        RES["resources:<br/>  - resources/Deployment_apps_v1_default_myapp.yaml<br/>  - resources/Service__v1_default_myapp.yaml"]
        
        PATCH["patches:<br/>  - path: patches/default--apps-v1--Deployment--myapp.patch.yaml<br/>    target:<br/>      kind: Deployment<br/>      name: myapp"]
        
        LABELS["commonLabels:<br/>  migrated-with: crane<br/>  environment: production"]
        
        IMAGES["images:<br/>  - name: mysql:8.0<br/>    newName: registry.redhat.io/rhel8/mysql-80<br/>    newTag: latest"]
    end
    
    API --> NS
    NS --> RES
    RES --> PATCH
    PATCH --> LABELS
    LABELS --> IMAGES
    
    style Kust fill:#f3e5f5
    style API fill:#e1f5ff
    style NS fill:#fff9c4
    style RES fill:#e8f5e9
    style PATCH fill:#ffe0b2
    style LABELS fill:#f8bbd0
    style IMAGES fill:#e1bee7
```

## 6. Sequential Consistency - What Does It Mean?

```mermaid
flowchart TB
    subgraph S1["Stage 1: KubernetesPlugin"]
        direction LR
        S1IN["Input:<br/>Deployment with UID,<br/>resourceVersion,<br/>status"]
        S1PROC["Plugin generates patch:<br/>- Remove metadata.uid<br/>- Remove metadata.resourceVersion<br/>- Remove status"]
        S1OUT["Output:<br/>Clean Deployment<br/>without cluster metadata"]
        S1IN --> S1PROC --> S1OUT
    end
    
    MATERIALIZE1["kubectl kustomize<br/>🔸 MATERIALIZATION"]
    
    subgraph S2["Stage 2: OpenshiftPlugin"]
        direction LR
        S2IN["Input:<br/>❗ Already contains only<br/>clean YAML without UID"]
        S2PROC["Plugin generates patch:<br/>- Convert Route → Ingress"]
        S2OUT["Output:<br/>Kubernetes-ready<br/>resources"]
        S2IN --> S2PROC --> S2OUT
    end
    
    MATERIALIZE2["kubectl kustomize<br/>🔸 MATERIALIZATION"]
    
    subgraph S3["Stage 3: CustomEdits"]
        direction LR
        S3IN["Input:<br/>❗ Sees already converted<br/>Ingress, not Route"]
        S3PROC["Manual edits:<br/>- Add labels<br/>- Change namespace"]
        S3OUT["Output:<br/>Final YAML"]
        S3IN --> S3PROC --> S3OUT
    end
    
    S1OUT --> MATERIALIZE1
    MATERIALIZE1 --> S2IN
    S2OUT --> MATERIALIZE2
    MATERIALIZE2 --> S3IN
    
    style S1 fill:#e3f2fd
    style S2 fill:#e3f2fd
    style S3 fill:#fff3e0
    style MATERIALIZE1 fill:#ffeb3b,stroke:#f57c00,stroke-width:3px
    style MATERIALIZE2 fill:#ffeb3b,stroke:#f57c00,stroke-width:3px
    style S1OUT fill:#c8e6c9
    style S2OUT fill:#c8e6c9
    style S3OUT fill:#c8e6c9
```

## 7. Whiteout Pattern - Deleting Resources

```mermaid
flowchart TB
    subgraph Export["export/resources/"]
        E1[Deployment A]
        E2[Deployment B ❌]
        E3[Service A]
    end
    
    subgraph S1["Stage 1: Plugin marks for deletion"]
        S1K["kustomization.yaml:<br/>resources:<br/>  - Deployment_A.yaml<br/>  - Service_A.yaml<br/># Deployment_B not in list = WHITEOUT"]
    end
    
    subgraph W1[".work/stage1/output/"]
        W1R1[Deployment A ✅]
        W1R2[Deployment B MISSING]
        W1R3[Service A ✅]
    end
    
    subgraph S2["Stage 2: Doesn't see deleted"]
        S2R["resources/<br/>❗ Contains only A and Service<br/>Deployment B doesn't exist"]
    end
    
    Export --> S1
    S1 -->|materialization| W1
    W1 -->|copies| S2
    
    style Export fill:#fff4e1
    style S1 fill:#e3f2fd
    style W1 fill:#f1f8e9
    style S2 fill:#e3f2fd
    style W1R2 fill:#ffcdd2,stroke:#c62828,stroke-width:3px
```

## 8. Debugging with .work Directory

```mermaid
flowchart TB
    subgraph Debug[".work/ Directory - For Debugging"]
        direction TB
        
        subgraph W1["10_KubernetesPlugin/"]
            W1I[input/<br/>What stage 1 read]
            W1O[output/<br/>What stage 1 produced]
        end
        
        subgraph W2["20_OpenshiftPlugin/"]
            W2I[input/<br/>What stage 2 read<br/>❗ = output of stage 1]
            W2O[output/<br/>What stage 2 produced]
        end
        
        CMD1["diff -r .work/10_KubernetesPlugin/input/<br/>      .work/10_KubernetesPlugin/output/<br/><br/>🔍 Compare input vs output"]
        
        CMD2["ls .work/20_OpenshiftPlugin/input/<br/><br/>🔍 What stage 2 received as input"]
    end
    
    W1I -.->|use for debug| CMD1
    W1O -.->|use for debug| CMD1
    W2I -.->|use for debug| CMD2
    
    style Debug fill:#f1f8e9
    style W1 fill:#e3f2fd
    style W2 fill:#e3f2fd
    style CMD1 fill:#fff9c4
    style CMD2 fill:#fff9c4
```

## 9. Best Practice - Stage Order

```mermaid
flowchart LR
    GOOD["✅ CORRECT"]
    subgraph Correct["Correct order"]
        direction TB
        C1[10_KubernetesPlugin<br/>Plugin - cleanup]
        C2[20_OpenshiftPlugin<br/>Plugin - conversion]
        C3[50_CustomEdits<br/>Manual - tweaks]
        C1 --> C2 --> C3
    end
    
    BAD["❌ WRONG"]
    subgraph Wrong["Wrong order"]
        direction TB
        W1[10_KubernetesPlugin]
        W2[50_CustomEdits<br/>⚠️ Manual stage in the middle]
        W3[20_OpenshiftPlugin<br/>⚠️ Plugin added later]
        W1 --> W2 --> W3
    end
    
    PROBLEM["🔴 Problem:<br/>Stage 50 has stale data from stage 10,<br/>not current from stage 20!<br/>--force will delete manual edits!"]
    
    GOOD --> Correct
    BAD --> Wrong
    Wrong -.-> PROBLEM
    
    style GOOD fill:#c8e6c9
    style Correct fill:#e8f5e9
    style BAD fill:#ffcdd2
    style Wrong fill:#ffebee
    style PROBLEM fill:#ef5350,color:#fff
    style C3 fill:#fff9c4
    style W2 fill:#ffccbc,stroke:#d84315,stroke-width:2px
```

## 10. Example Use Case - Cross-Platform Migration

```mermaid
flowchart TB
    START[OpenShift Source Cluster]
    
    EXPORT["crane export<br/>📥 export/resources/"]
    
    subgraph Transform["crane transform"]
        direction TB
        
        T1["Stage 1: 10_KubernetesPlugin<br/>🧹 Remove cluster metadata"]
        T2["Stage 2: 20_OpenshiftPlugin<br/>🔄 Convert OpenShift → K8s"]
        T3["Stage 3: 50_CustomEdits<br/>✏️ Namespace + Labels + Images"]
        
        T1 --> T2 --> T3
    end
    
    APPLY["crane apply<br/>📤 output/output.yaml"]
    
    KUBECTL["kubectl apply -f output/output.yaml"]
    
    END[Kubernetes Target Cluster]
    
    START --> EXPORT
    EXPORT --> Transform
    Transform --> APPLY
    APPLY --> KUBECTL
    KUBECTL --> END
    
    style START fill:#e74c3c,color:#fff
    style EXPORT fill:#fff4e1
    style Transform fill:#e8f5e9
    style T1 fill:#e3f2fd
    style T2 fill:#e3f2fd
    style T3 fill:#fff3e0
    style APPLY fill:#f3e5f5
    style KUBECTL fill:#b39ddb
    style END fill:#4caf50,color:#fff
```

## Legend

| Symbol | Meaning |
|--------|---------|
| 📥 | Input/Import |
| 📤 | Output/Export |
| 🔄 | Plugin Stage (auto-regeneration) |
| ✏️ | Pass-Through Stage (manual editing) |
| 🧹 | Cleanup operation |
| ❗ | Materialized YAML (important concept) |
| ✅ | Feature supported |
| ❌ | Feature not supported |
| ⚠️ | Warning |
| 🔴 | Error/Problem |
| 🔍 | Debugging command |

## How to Use These Diagrams

These Mermaid diagrams can be embedded in:
- **GitHub Markdown** - automatically rendered
- **GitLab** - supports Mermaid
- **VS Code** - with Mermaid Preview extension
- **Notion** - by importing Markdown
- **Online Mermaid Editor** - https://mermaid.live/

For export to PNG/SVG use [Mermaid Live Editor](https://mermaid.live/).
