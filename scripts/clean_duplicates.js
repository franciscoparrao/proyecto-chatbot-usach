// Script para limpiar duplicados en MongoDB
// Prioriza mantener documentos con email

print("=== LIMPIEZA DE DUPLICADOS EN MONGODB ===");
print("Iniciando análisis...\n");

// Contar estado inicial
var totalInicial = db.articulos_wos.countDocuments();
var conEmailInicial = db.articulos_wos.countDocuments({email: {$exists: true, $ne: null}});
print("Total documentos inicial: " + totalInicial);
print("Con email inicial: " + conEmailInicial);

// Encontrar duplicados agrupando por título
var duplicadosEliminados = 0;
var titulosProcesados = 0;

db.articulos_wos.aggregate([
    {
        $group: {
            _id: "$originaltitle",
            docs: { $push: { id: "$_id", hasEmail: { $cond: [{$and: [{$ne: ["$email", null]}, {$ne: ["$email", ""]}]}, 1, 0] } } },
            count: { $sum: 1 }
        }
    },
    {
        $match: { count: { $gt: 1 } }
    }
]).forEach(function(grupo) {
    titulosProcesados++;
    
    // Ordenar: primero los que tienen email
    var docsOrdenados = grupo.docs.sort(function(a, b) {
        return b.hasEmail - a.hasEmail;
    });
    
    // Mantener el primero (el que tiene email si existe), eliminar el resto
    var idsAEliminar = [];
    for (var i = 1; i < docsOrdenados.length; i++) {
        idsAEliminar.push(docsOrdenados[i].id);
    }
    
    if (idsAEliminar.length > 0) {
        db.articulos_wos.deleteMany({ _id: { $in: idsAEliminar } });
        duplicadosEliminados += idsAEliminar.length;
    }
    
    if (titulosProcesados % 100 === 0) {
        print("Procesados " + titulosProcesados + " grupos de duplicados...");
    }
});

print("\n=== RESULTADOS ===");
print("Grupos de duplicados procesados: " + titulosProcesados);
print("Documentos eliminados: " + duplicadosEliminados);

// Contar estado final
var totalFinal = db.articulos_wos.countDocuments();
var conEmailFinal = db.articulos_wos.countDocuments({email: {$exists: true, $ne: null}});
print("\nTotal documentos final: " + totalFinal);
print("Con email final: " + conEmailFinal);
print("Reducción: " + (totalInicial - totalFinal) + " documentos");

// Verificar integridad
var titulosUnicos = db.articulos_wos.distinct("originaltitle").length;
print("\nTítulos únicos finales: " + titulosUnicos);

print("\n=== LIMPIEZA COMPLETADA ===");