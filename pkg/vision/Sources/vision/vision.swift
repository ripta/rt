import Cocoa
import Vision

@available(macOS 10.15, *)
@_cdecl("recognizeText")
public func recognizeText(in bytes: UnsafeRawPointer, count: Int) -> Bool {
    let data = Data(bytes: bytes, count: count)
    guard let img = NSImage(data: data) else {
        // "Invalid image data"
        return false
    }

    guard let cgImg = img.cgImage(forProposedRect: &img.alignmentRect, context: nil, hints: nil) else {
        // "Failed to convert UIKit image into a Quartz image"
        return false
    }

    let reqHnd = VNImageRequestHandler(cgImage: cgImg)

    let req = VNRecognizeTextRequest(completionHandler: recognizeTextHnd)

    try? reqHnd.perform([req])

    // "Recognized text"
    return true
}

@available(macOS 10.15, *)
func recognizeTextHnd(req: VNRequest, error: Error?) {
    guard let obs = req.results as? [VNRecognizedTextObservation] else {
        return
    }

    let recogs = obs.compactMap { observation in
        return observation.topCandidates(1).first?.string
    }

    puts("OK")
    //    processRecognizedText(recogs)
}
