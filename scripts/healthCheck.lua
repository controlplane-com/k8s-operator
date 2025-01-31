hs = {}

function isDownstreamOnly(o)
  return o.status and o.status.operator and o.status.operator.downstreamOnly == true
end

if obj.metadata
  and obj.metadata.deletionTimestamp ~= nil
  and obj.status
  and obj.status.operator
  and obj.status.operator.validationError ~= nil
then
    hs.status = "Progressing"
    hs.message = obj.status.operator.validationError
    return hs
end

if obj.status
  and obj.status.operator
  and obj.status.operator.validationError ~= nil
then
  hs.status = "Degraded"
  hs.message = obj.status.operator.validationError
  return hs
end

if not isDownstreamOnly(obj)
   and (
     not obj.status
     or not obj.status.operator
     or not obj.status.operator.lastSyncedGeneration
     or obj.status.operator.lastSyncedGeneration ~= obj.metadata.generation
   )
then
  hs.status = "Progressing"
  return hs
end

if obj.status
  and obj.status.phase
  and obj.status.phase == "Unhealthy"
then
  hs.status = "Degraded"
  hs.message = ""
  return hs
end

if obj.status
  and obj.status.phase
  and obj.status.phase == "Suspended"
then
  hs.status = "Suspended"
  hs.message = ""
  return hs
end

if obj.status
  and obj.status.phase
  and obj.status.phase == "Pending"
then
  hs.status = "Progressing"
  hs.message = ""
  return hs
end

if obj.status
  and obj.status.phase
  and obj.status.phase == "Ready"
then
  hs.status = "Healthy"
  hs.message = ""
  return hs
end

if not obj.status or not obj.status.phase
then
    hs.status = "Healthy"
    return hs
end

hs.status = "Progressing"
return hs